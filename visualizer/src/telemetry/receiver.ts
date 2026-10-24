import type { LoadBalancerState } from '../types';
import { hydrateSnapshot, idleSnapshot } from './hydrate.ts';
import type { SimulationSnapshot, TopologyDefinition } from './types.ts';

export interface TelemetryView {
  data: LoadBalancerState | null;
  loading: boolean;
  error: string | null;
}

interface ReceivedFrame {
  snapshot: SimulationSnapshot;
  sequence: number;
}

interface DefinitionRequest {
  frame: ReceivedFrame;
  generation: number;
  controller: AbortController;
}

type DefinitionLoader = (signal: AbortSignal) => Promise<TopologyDefinition>;

// One HTTP request follows the newest independent frame, without cancelling on each revision.
export class TelemetryReceiver {
  private readonly load: DefinitionLoader;
  private readonly changed: (view: TelemetryView) => void;
  private latest: ReceivedFrame | null = null;
  private previous: ReceivedFrame | null = null;
  private matched: { frame: ReceivedFrame; data: LoadBalancerState } | null = null;
  private definitions = new Map<string, TopologyDefinition>();
  private request: DefinitionRequest | null = null;
  private requestedKey: string | null = null;
  private generation = 0;
  private sequence = 0;
  private disposed = false;
  private failure: string | null = null;
  private view: TelemetryView = { data: null, loading: false, error: null };

  constructor(load: DefinitionLoader, changed: (view: TelemetryView) => void) {
    this.load = load;
    this.changed = changed;
  }

  get state(): TelemetryView {
    return this.view;
  }

  receive(snapshot: SimulationSnapshot) {
    if (this.disposed) return;
    const oldRun = this.latest?.snapshot.simulation.runId;
    if (snapshot.simulation.status === 'idle' || (oldRun && oldRun !== snapshot.simulation.runId)) {
      this.clear();
    }

    const frame = { snapshot, sequence: ++this.sequence };
    if (this.latest?.snapshot.topology.revision !== snapshot.topology.revision) {
      this.previous = this.latest;
    }
    this.latest = frame;

    if (snapshot.simulation.status === 'idle') {
      this.matched = { frame, data: idleSnapshot(snapshot) };
    } else {
      this.hydrate(frame);
    }
    this.publish();
    this.fetchDefinitions();
  }

  // Reconnect invalidates even identical run IDs, which a restarted backend may reuse.
  reset() {
    if (this.disposed) return;
    this.clear();
    this.view = { ...this.view, loading: false, error: null };
    this.changed(this.view);
  }

  retry() {
    if (this.disposed || this.request) return;
    const revision = this.latest?.snapshot.topology.revision;
    if (revision) this.definitions.delete(revision);
    this.requestedKey = null;
    this.failure = null;
    this.publish();
    this.fetchDefinitions();
  }

  dispose() {
    this.clear();
    this.disposed = true;
  }

  private clear() {
    this.generation++;
    this.request?.controller.abort();
    this.request = null;
    this.latest = null;
    this.previous = null;
    this.matched = null;
    this.definitions.clear();
    this.requestedKey = null;
    this.failure = null;
  }

  private hydrate(frame: ReceivedFrame) {
    const definition = this.definitions.get(frame.snapshot.topology.revision);
    if (!definition || frame.sequence < (this.matched?.frame.sequence ?? 0)) return;
    try {
      const data = hydrateSnapshot(frame.snapshot, definition);
      this.matched = { frame, data };
      this.failure = null;
    } catch (error) {
      this.failure = error instanceof Error ? error.message : 'Cannot combine topology measurements.';
    }
  }

  private fetchDefinitions() {
    const latest = this.latest;
    if (!latest || this.disposed || this.request || latest.snapshot.simulation.status === 'idle') return;
    const { runId } = latest.snapshot.simulation;
    const { revision } = latest.snapshot.topology;
    if (this.definitions.has(revision)) return;
    const key = JSON.stringify([runId, revision]);
    if (key === this.requestedKey) return;

    const request: DefinitionRequest = {
      frame: latest,
      generation: this.generation,
      controller: new AbortController(),
    };
    this.request = request;
    this.requestedKey = key;
    this.failure = null;
    this.publish();

    this.load(request.controller.signal).then((definition) => {
      if (!this.isCurrent(request)) return;
      if (definition.runId !== this.latest?.snapshot.simulation.runId) return;
      this.definitions.set(definition.revision, definition);

      // A response can match an earlier frame while newer topology is still arriving.
      const candidates = [this.latest, this.previous, request.frame];
      const matching = candidates.find((frame) => (
        frame?.snapshot.simulation.runId === definition.runId
        && frame.snapshot.topology.revision === definition.revision
      ));
      if (matching) this.hydrate(matching);
      this.trimDefinitions();
    }).catch((error: unknown) => {
      if (!this.isCurrent(request)) return;
      this.failure = error instanceof Error ? error.message : 'Cannot load topology definitions.';
    }).finally(() => {
      if (!this.isCurrent(request)) return;
      this.request = null;
      this.publish();
      this.fetchDefinitions();
    });
  }

  private isCurrent(request: DefinitionRequest): boolean {
    return !this.disposed && this.request === request && request.generation === this.generation;
  }

  private trimDefinitions() {
    const displayed = this.matched?.frame.snapshot.topology.revision;
    for (const revision of this.definitions.keys()) {
      if (this.definitions.size <= 2) break;
      if (revision !== displayed) this.definitions.delete(revision);
    }
  }

  private publish() {
    const frame = this.latest;
    if (!frame || this.disposed) return;
    const snapshot = frame.snapshot;
    const sameRun = this.matched?.frame.snapshot.simulation.runId === snapshot.simulation.runId;
    const data: LoadBalancerState = {
      ...(sameRun ? this.matched?.data : undefined),
      mode: snapshot.mode,
      algorithm: snapshot.algorithm,
      simulation: snapshot.simulation,
      servers: sameRun ? this.matched?.data.servers ?? [] : [],
    };
    this.view = {
      data,
      loading: snapshot.simulation.status !== 'idle'
        && this.matched?.frame.sequence !== frame.sequence && this.failure === null,
      error: this.failure,
    };
    this.changed(this.view);
  }
}
