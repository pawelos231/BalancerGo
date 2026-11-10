import { telemetrySource } from '../telemetry/exact.ts';
import type { CurrentSimulation, Scenario } from '../scenarios/types.ts';
import type { SimulationSnapshot, TopologyDefinition } from '../telemetry/types.ts';
import { freezeTree } from './immutable.ts';
import {
  COLLECTION_VERSION,
  type CollectedRun,
  type CollectedMetadata,
  type CollectedSample,
  type CollectionLimits,
  type CollectionUsage,
  type ObservationMarker,
} from './types.ts';

export const COLLECTION_BYTE_BUDGET = 32 * 1024 * 1024;
export const COLLECTION_TERMINAL_RESERVE = 2 * 1024 * 1024;

interface CollectionOptions {
  byteBudget?: number;
  terminalReserveBytes?: number;
  maxRuns?: number;
  maxMarkersPerRun?: number;
}

type Mutable<T> = { -readonly [Key in keyof T]: T[Key] };
interface RunBuffer {
  data: Omit<Mutable<CollectedRun>, 'limits'> & { limits: Mutable<CollectionLimits> };
  samples: CollectedSample[];
  definitions: CollectedRun['definitions'][number][];
  markers: ObservationMarker[];
  sequences: Set<string>;
  revisions: Set<string>;
  bytes: number;
  metadataBytes: number;
}

// Covers fixed JSON keys, punctuation and bounded numeric counters in each header.
// Variable strings and array entries are charged separately, including a comma.
const RUN_HEADER_BYTES = 2048;
const COLLECTION_HEADER_BYTES = 512;
const encoder = new TextEncoder();
const byteLength = (json: string) => encoder.encode(json).byteLength;
const increment = (value: number) => Math.min(value + 1, Number.MAX_SAFE_INTEGER);

function requireTimestamp(value: number) {
  if (!Number.isFinite(value)) throw new Error('Collection timestamp must be finite.');
}

// No timers or view dependencies: each validated wire publication is offered here
// synchronously, before hydration and React's coalescing of live updates.
export class ResultsCollection {
  private readonly byteBudget: number;
  private readonly terminalReserveBytes: number;
  private readonly maxRuns: number;
  private readonly maxMarkersPerRun: number;
  private readonly runs = new Map<string, RunBuffer>();
  private bytes = COLLECTION_HEADER_BYTES;
  private memoryLimited = false;
  private runLimitReached = false;
  private droppedRunRecords = 0;
  private activeRunId: string | null = null;
  private connected = true;

  constructor(options: CollectionOptions = {}) {
    this.byteBudget = options.byteBudget ?? COLLECTION_BYTE_BUDGET;
    this.terminalReserveBytes = options.terminalReserveBytes ?? COLLECTION_TERMINAL_RESERVE;
    this.maxRuns = options.maxRuns ?? 8;
    this.maxMarkersPerRun = options.maxMarkersPerRun ?? 256;
    if (!Number.isSafeInteger(this.byteBudget) || this.byteBudget < RUN_HEADER_BYTES * 2
      || !Number.isSafeInteger(this.terminalReserveBytes) || this.terminalReserveBytes < 0
      || this.terminalReserveBytes >= this.byteBudget - COLLECTION_HEADER_BYTES
      || !Number.isSafeInteger(this.maxRuns) || this.maxRuns < 1
      || !Number.isSafeInteger(this.maxMarkersPerRun) || this.maxMarkersPerRun < 0) {
      throw new Error('Invalid collection limits.');
    }
  }

  get usage(): CollectionUsage {
    return Object.freeze({
      serializedBytes: this.bytes, byteBudget: this.byteBudget,
      terminalReserveBytes: this.terminalReserveBytes, runCount: this.runs.size,
      memoryLimited: this.memoryLimited, runLimitReached: this.runLimitReached,
      droppedRunRecords: this.droppedRunRecords,
    });
  }

  // A stopped run can legitimately have no delivered measurements.
  begin(runId: string, receivedAt = Date.now(), terminal = false): boolean {
    requireTimestamp(receivedAt);
    return this.run(runId, receivedAt, terminal) !== undefined;
  }

  snapshot(frame: SimulationSnapshot, receivedAt = Date.now(), finalSnapshot = false): boolean {
    requireTimestamp(receivedAt);
    if (frame.simulation.status === 'idle') return false;
    const { runId, publicationSequence, virtualTimeSeconds } = frame.simulation;
    const run = this.run(runId, receivedAt, finalSnapshot);
    if (!run) return false;
    if (finalSnapshot) {
      run.data.terminalPublicationSequence = publicationSequence;
      if (this.activeRunId === runId) this.activeRunId = null;
    }
    if (run.sequences.has(publicationSequence)) {
      if (finalSnapshot) run.data.limits.terminalSnapshotDropped = false;
      return false;
    }

    const highest = run.data.highestObservedSequence;
    if (!finalSnapshot && run.data.terminalPublicationSequence === null
      && (highest === null || BigInt(publicationSequence) > BigInt(highest))) {
      this.activeRunId = runId;
    }
    this.observe(run, publicationSequence, receivedAt);
    const sample: CollectedSample = {
      index: run.samples.length + 1, publicationSequence, virtualTimeSeconds, receivedAt,
      snapshot: telemetrySource(frame),
    };
    const json = JSON.stringify(sample);
    if (!this.charge(run, byteLength(json) + 1, finalSnapshot)) {
      run.data.limits.droppedSamples = increment(run.data.limits.droppedSamples);
      if (finalSnapshot) run.data.limits.terminalSnapshotDropped = true;
      return false;
    }
    // Serializing then decoding owns the source; later mutations of a fixture,
    // parsed transport tree, or hydrated view cannot rewrite archived measurements.
    run.samples.push(freezeTree(JSON.parse(json) as CollectedSample));
    run.sequences.add(publicationSequence);
    if (finalSnapshot) run.data.limits.terminalSnapshotDropped = false;
    return true;
  }

  definition(definition: TopologyDefinition, receivedAt = Date.now(), terminal = false): boolean {
    requireTimestamp(receivedAt);
    const run = this.run(definition.runId, receivedAt, terminal);
    if (!run || run.revisions.has(definition.revision)) return false;
    const json = JSON.stringify(telemetrySource(definition));
    if (!this.charge(run, byteLength(json) + 1, terminal)) {
      run.data.limits.droppedDefinitions = increment(run.data.limits.droppedDefinitions);
      return false;
    }
    run.definitions.push(freezeTree(JSON.parse(json) as CollectedRun['definitions'][number]));
    run.revisions.add(definition.revision);
    return true;
  }

  submitted(runId: string, scenario: Scenario, receivedAt = Date.now()): boolean {
    requireTimestamp(receivedAt);
    const run = this.run(runId, receivedAt, false);
    if (!run || run.data.metadata !== null) return false;
    return this.captureMetadata(run, {
      provenance: 'submitted', receivedAt, scenario: telemetrySource(scenario), experiment: null,
    });
  }

  metadata(current: CurrentSimulation, receivedAt = Date.now()): boolean {
    requireTimestamp(receivedAt);
    const run = this.run(current.runId, receivedAt, false);
    if (!run) return false;
    const source = telemetrySource(current);
    const previous = run.data.metadata;
    if (previous?.provenance === 'backend'
      && JSON.stringify(previous.scenario) === JSON.stringify(source.scenario)
      && JSON.stringify(previous.experiment) === JSON.stringify(source.experiment)) return false;
    return this.captureMetadata(run, {
      provenance: 'backend', receivedAt, scenario: source.scenario, experiment: source.experiment,
    });
  }

  disconnected(receivedAt = Date.now()) {
    requireTimestamp(receivedAt);
    if (!this.connected) return;
    this.connected = false;
    this.connectionMarker('disconnected', receivedAt);
  }

  reconnected(receivedAt = Date.now()) {
    requireTimestamp(receivedAt);
    if (this.connected) return;
    this.connected = true;
    this.connectionMarker('reconnected', receivedAt);
  }

  get(runId: string): CollectedRun | undefined {
    const run = this.runs.get(runId);
    if (!run) return undefined;
    return Object.freeze({
      ...run.data, samples: Object.freeze([...run.samples]),
      definitions: Object.freeze([...run.definitions]), markers: Object.freeze([...run.markers]),
      limits: Object.freeze({ ...run.data.limits }),
    });
  }

  release(runId: string) {
    const run = this.runs.get(runId);
    if (!run) return;
    this.bytes -= run.bytes;
    this.runs.delete(runId);
    if (this.activeRunId === runId) this.activeRunId = null;
  }

  private run(runId: string, receivedAt: number, terminal: boolean): RunBuffer | undefined {
    const existing = this.runs.get(runId);
    if (existing) return existing;
    const bytes = RUN_HEADER_BYTES + byteLength(JSON.stringify(runId));
    const fits = this.fits(bytes, terminal);
    if (this.runs.size >= this.maxRuns || !fits) {
      this.droppedRunRecords = increment(this.droppedRunRecords);
      if (this.runs.size >= this.maxRuns) this.runLimitReached = true;
      if (!fits) this.memoryLimited = true;
      return undefined;
    }
    const samples: CollectedSample[] = [];
    const definitions: RunBuffer['definitions'] = [];
    const markers: ObservationMarker[] = [];
    const run: RunBuffer = {
      data: {
        collectionVersion: COLLECTION_VERSION, runId, createdAt: receivedAt,
        samples, definitions, markers, firstObservedSequence: null, firstObservedAt: null,
        highestObservedSequence: null, terminalPublicationSequence: null, metadata: null,
        limits: {
          memoryLimited: false, droppedSamples: 0, droppedDefinitions: 0, droppedMetadata: 0,
          droppedMarkers: 0, markerLimitReached: false, terminalSnapshotDropped: false,
        },
      },
      samples, definitions, markers, sequences: new Set(), revisions: new Set(), bytes, metadataBytes: 0,
    };
    this.runs.set(runId, run);
    this.bytes += bytes;
    return run;
  }

  private fits(bytes: number, terminal: boolean) {
    const limit = this.byteBudget - (terminal ? 0 : this.terminalReserveBytes);
    return this.bytes + bytes <= limit;
  }

  private charge(run: RunBuffer, bytes: number, terminal = false): boolean {
    if (!this.fits(bytes, terminal)) {
      this.memoryLimited = true;
      run.data.limits.memoryLimited = true;
      return false;
    }
    this.bytes += bytes;
    run.bytes += bytes;
    return true;
  }

  private captureMetadata(run: RunBuffer, metadata: CollectedMetadata): boolean {
    const json = JSON.stringify(metadata);
    const bytes = byteLength(json) + 1;
    const growth = bytes - run.metadataBytes;
    if (growth > 0 && !this.charge(run, growth)) {
      run.data.limits.droppedMetadata = increment(run.data.limits.droppedMetadata);
      return false;
    }
    if (growth < 0) {
      this.bytes += growth;
      run.bytes += growth;
    }
    run.metadataBytes = bytes;
    run.data.metadata = freezeTree(JSON.parse(json) as CollectedMetadata);
    return true;
  }

  private marker(run: RunBuffer, marker: ObservationMarker) {
    if (run.markers.length >= this.maxMarkersPerRun) {
      run.data.limits.markerLimitReached = true;
      run.data.limits.droppedMarkers = increment(run.data.limits.droppedMarkers);
      return;
    }
    if (!this.charge(run, byteLength(JSON.stringify(marker)) + 1)) {
      run.data.limits.droppedMarkers = increment(run.data.limits.droppedMarkers);
      return;
    }
    run.markers.push(Object.freeze(marker));
  }

  private observe(run: RunBuffer, sequence: string, receivedAt: number) {
    const previous = run.data.highestObservedSequence;
    if (previous === null) {
      run.data.firstObservedSequence = sequence;
      run.data.firstObservedAt = receivedAt;
      if (sequence !== '1') this.marker(run, { kind: 'lateJoin', publicationSequence: sequence, receivedAt });
    } else if (BigInt(sequence) > BigInt(previous) + 1n) {
      this.marker(run, { kind: 'gap', afterSequence: previous, beforeSequence: sequence, receivedAt });
    }
    if (previous === null || BigInt(sequence) > BigInt(previous)) {
      run.data.highestObservedSequence = sequence;
    }
  }

  private connectionMarker(kind: 'disconnected' | 'reconnected', receivedAt: number) {
    const run = this.activeRunId === null ? undefined : this.runs.get(this.activeRunId);
    if (run) this.marker(run, { kind, receivedAt });
  }
}
