import type { ExactTelemetry } from '../telemetry/exact.ts';
import type { NodeTelemetry } from '../telemetry/types.ts';
import { freezeTree } from './immutable.ts';
import {
  RUN_RESULT_VERSION,
  type CollectedRun,
  type CollectedSample,
  type HostCPUSummary,
  type PublicationRange,
  type ResultCoverage,
  type RunResult,
  type RunSummary,
  type SampleReference,
} from './types.ts';

export interface AssembleRunOptions {
  readonly id: string;
  readonly completion: RunResult['completion'];
}

function reference(sample: CollectedSample): SampleReference {
  return {
    index: sample.index, publicationSequence: sample.publicationSequence,
    virtualTimeSeconds: sample.virtualTimeSeconds, receivedAt: sample.receivedAt,
    topologyRevision: sample.snapshot.topology.revision,
  };
}

// Order by publication identity, never by receipt order or virtual timestamp.
function publicationOrder(left: CollectedSample, right: CollectedSample): number {
  const a = BigInt(left.publicationSequence);
  const b = BigInt(right.publicationSequence);
  return a < b ? -1 : a > b ? 1 : 0;
}

function lastObservations<T extends { id: string }>(
  samples: readonly CollectedSample[],
  select: (sample: CollectedSample) => readonly T[],
) {
  const values = new Map<string, { id: string; observation: SampleReference; value: T }>();
  let present = new Set<string>();
  for (const sample of samples) {
    const current = select(sample);
    present = new Set(current.map((value) => value.id));
    for (const value of current) {
      values.set(value.id, { id: value.id, observation: reference(sample), value });
    }
  }
  return [...values.values()].map((value) => ({
    ...value, presentInSummarySample: present.has(value.id),
  }));
}

function hostCPU(node: ExactTelemetry<NodeTelemetry>): HostCPUSummary {
  return {
    basis: 'hostLifetimeMean', utilizationRatio: node.metrics.hostUtilization ?? null,
    busyTimeSeconds: node.metrics.hostBusyTimeSeconds ?? null,
    availableCoreTimeSeconds: node.metrics.hostAvailableCoreTimeSeconds ?? null,
  };
}

function approximateRate(bytes: number | string | undefined, seconds: number | string): number | null {
  if (bytes === undefined || Number(seconds) <= 0) return null;
  const rate = Number(bytes) / Number(seconds);
  return Number.isFinite(rate) ? rate : null;
}

function summarize(run: CollectedRun, samples: readonly CollectedSample[]): RunSummary {
  const latest = samples[samples.length - 1];
  const simulation = latest?.snapshot.simulation;
  const definitions = new Map(run.definitions.map((definition) => [
    definition.revision, new Map(definition.nodes.map((node) => [node.id, node])),
  ]));
  const nodes = lastObservations(samples, (sample) => sample.snapshot.topology.nodes).map((node) => {
    const definition = definitions.get(node.observation.topologyRevision)?.get(node.id);
    return {
      ...node, kind: definition?.kind ?? null, groupId: definition?.groupId ?? null,
      cpu: hostCPU(node.value),
    };
  });
  const servers = lastObservations(samples, (sample) => sample.snapshot.servers).map((server) => ({
    ...server,
    cpu: {
      basis: 'hostLifetimeMean' as const, utilizationRatio: server.value.cpuUsage ?? null,
      busyTimeSeconds: null, availableCoreTimeSeconds: null,
    },
  }));
  const clients = lastObservations(samples, (sample) => sample.snapshot.servers.flatMap((server) => (
    server.clients.map((client) => ({ ...client, serverId: server.id }))
  )));
  const ingress = lastObservations(samples, (sample) => sample.snapshot.topology.ingress
    ? [{ id: 'ingress', stats: sample.snapshot.topology.ingress.stats }] : [])[0];

  return {
    basis: latest === undefined ? 'unavailable'
      : latest.publicationSequence === run.terminalPublicationSequence ? 'terminal' : 'lastObservation',
    observation: latest ? reference(latest) : null,
    payload: simulation?.payload ?? null,
    transfers: simulation?.transfers ?? null,
    performance: simulation?.performance ?? null,
    payloadRate: simulation?.payload ? {
      basis: 'runVirtualTime', virtualTimeSeconds: simulation.virtualTimeSeconds,
      approximateReceivedBytesPerSecond: approximateRate(
        simulation.payload.receivedBytes, simulation.virtualTimeSeconds,
      ),
      approximateAcknowledgedBytesPerSecond: approximateRate(
        simulation.payload.acknowledgedBytes, simulation.virtualTimeSeconds,
      ),
    } : null,
    // The contract has per-backend connection-lifetime p95, no global transfer percentile.
    transferLatencyP95Seconds: null,
    transferLatencyP99Seconds: null,
    resources: {
      nodes, servers, clients,
      links: lastObservations(samples, (sample) => sample.snapshot.topology.links),
      ingress: ingress ? { ...ingress, value: ingress.value.stats } : null,
      queues: lastObservations(samples, (sample) => sample.snapshot.topology.ingress?.queues ?? []),
    },
  };
}

function publicationCoverage(run: CollectedRun, samples: readonly CollectedSample[]) {
  const ranges: PublicationRange[] = [];
  let next = 1n;
  let count = 0n;
  function gap(first: bigint, last: bigint) {
    if (first > last) return;
    ranges.push({ first: first.toString(), last: last.toString() });
    count += last - first + 1n;
  }
  for (const sample of samples) {
    const sequence = BigInt(sample.publicationSequence);
    gap(next, sequence - 1n);
    next = sequence + 1n;
  }
  // Even rejected samples advance the observed boundary in the collector.
  const highest = BigInt(run.highestObservedSequence ?? '0');
  const terminal = BigInt(run.terminalPublicationSequence ?? '0');
  gap(next, highest > terminal ? highest : terminal);
  return { ranges, count: count.toString() };
}

function coverage(
  run: CollectedRun, samples: readonly CollectedSample[], context: RunResult['context'], summary: RunSummary,
): ResultCoverage {
  const publications = publicationCoverage(run, samples);
  const revisions = new Set(run.definitions.map((definition) => definition.revision));
  const missingDefinitionRevisions = [...new Set(samples.map((sample) => sample.snapshot.topology.revision))]
    .filter((revision) => !revisions.has(revision));
  const disconnections = run.markers.filter((marker) => marker.kind === 'disconnected').length;
  const first = run.firstObservedSequence === null ? 1n : BigInt(run.firstObservedSequence);
  const terminalSampleMissing = run.terminalPublicationSequence === null
    || !samples.some((sample) => sample.publicationSequence === run.terminalPublicationSequence);
  const limits = run.limits;
  const missingScenario = context.scenario === null;
  const missingAuthoritativeScenario = context.scenarioProvenance !== 'backend';
  const missingExperiment = context.experiment === null;
  return {
    complete: publications.ranges.length === 0 && disconnections === 0
      && missingDefinitionRevisions.length === 0 && !missingAuthoritativeScenario && !missingExperiment
      && !terminalSampleMissing && summary.basis === 'terminal'
      && !limits.memoryLimited && limits.droppedSamples === 0 && limits.droppedDefinitions === 0
      && limits.droppedMetadata === 0 && limits.droppedMarkers === 0 && !limits.markerLimitReached,
    joinedAfterStart: first > 1n,
    publicationsBeforeFirstReceipt: first > 1n ? { first: '1', last: (first - 1n).toString() } : null,
    unrecordedPublications: publications.ranges,
    unrecordedPublicationCount: publications.count,
    disconnections, missingDefinitionRevisions, missingScenario, missingAuthoritativeScenario, missingExperiment,
    metadataAfterFirstSample: run.firstObservedAt !== null && context.scenarioReceivedAt !== null
      && context.scenarioReceivedAt > run.firstObservedAt,
    terminalSampleMissing, memoryLimited: limits.memoryLimited, limits,
  };
}

// Assembly is pure: callers decide whether a lifecycle event is eligible for saving.
// A new immutable artifact owns all raw data; later metadata cannot rewrite it.
export function assembleRunResult(run: CollectedRun, options: AssembleRunOptions): RunResult {
  if (!options.id.trim()) throw new Error('A local result ID is required.');
  if (!Number.isFinite(options.completion.receivedAt)) throw new Error('Completion timestamp must be finite.');
  if (options.completion.reason !== 'finished' && options.completion.reason !== 'stopped') {
    throw new Error('Only finished or stopped runs can be assembled.');
  }
  const samples = [...run.samples].sort(publicationOrder);
  const terminalIndex = samples.findIndex((sample) => (
    sample.publicationSequence === run.terminalPublicationSequence
  ));
  const summarySamples = terminalIndex < 0 ? samples : samples.slice(0, terminalIndex + 1);
  const summary = summarize(run, summarySamples);
  const latest = summarySamples[summarySamples.length - 1];
  const metadata = run.metadata;
  const backendExperiment = metadata?.provenance === 'backend' ? metadata.experiment : null;
  const snapshotExperiment = latest?.snapshot.simulation.status !== 'idle'
    ? latest?.snapshot.simulation.experiment : undefined;
  const experiment = backendExperiment ?? snapshotExperiment ?? null;
  const scenario = metadata?.scenario ?? null;
  const context: RunResult['context'] = {
    scenario, scenarioProvenance: metadata?.provenance ?? 'missing',
    scenarioReceivedAt: metadata?.receivedAt ?? null,
    experiment,
    experimentProvenance: backendExperiment ? 'backend' : snapshotExperiment ? 'snapshot' : 'missing',
    experimentReceivedAt: backendExperiment
      ? metadata?.receivedAt ?? null : snapshotExperiment ? latest.receivedAt : null,
    seed: scenario?.fixed.execution.seed ?? null,
    publicationInterval: {
      clock: 'real', configuredNanoseconds: scenario?.fixed.execution.snapshotIntervalNanoseconds ?? null,
    },
  };
  const result: RunResult = {
    resultVersion: RUN_RESULT_VERSION, id: options.id, runId: run.runId,
    completion: { ...options.completion }, context, summary,
    coverage: coverage(run, samples, context, summary), collection: run,
  };
  return freezeTree(JSON.parse(JSON.stringify(result)) as RunResult);
}
