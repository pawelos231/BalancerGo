import type { ExactTelemetry } from '../telemetry/exact.ts';
import type { NodeTelemetry, TopologyDefinition } from '../telemetry/types.ts';
import type { CollectedSample, RunResult } from './types.ts';

type Definition = ExactTelemetry<TopologyDefinition>;
type Node = ExactTelemetry<NodeTelemetry>;
type Numeric = number | string | undefined;

export interface ChartBackend {
  readonly id: string;
  readonly label: string;
}

export interface ChartSeries {
  readonly key: string;
  readonly label: string;
}

export interface ChartSample {
  readonly index: number;
  readonly publicationSequence: string;
  readonly virtualTimeSeconds: number;
  readonly topologyRevision: string;
  readonly breakBefore: boolean;
  readonly notes: readonly string[];
  readonly wireWindowSeconds: number | null;
  readonly ackIntervalSeconds: number | null;
  readonly values: Readonly<Record<string, number | null>>;
}

export interface ChartDefinition {
  readonly title: string;
  readonly description: string;
  readonly unit: string;
  readonly series: readonly ChartSeries[];
}

export interface ChartVertex {
  readonly sample: ChartSample;
  readonly value: number;
}

export interface ChartTrace {
  readonly key: string;
  readonly segments: readonly (readonly ChartVertex[])[];
}

export const resultChartDefinitions: readonly ChartDefinition[] = [
  {
    title: 'Wire traffic and payload goodput',
    description: 'LB RX / TX are native wire-byte rates over their reported virtual window. '
      + 'ACK goodput is the acknowledged payload byte delta per adjacent virtual interval.',
    unit: 'B / virtual s',
    series: [
      { key: 'wireRX', label: 'LB wire RX' },
      { key: 'wireTX', label: 'LB wire TX' },
      { key: 'ackGoodput', label: 'ACK payload goodput' },
    ],
  },
  {
    title: 'Queue depths',
    description: 'Waiting packets at each retained observation; shared input sides, LB outputs and host CPU queues.',
    unit: 'packets',
    series: [
      { key: 'clientQueued', label: 'Client input' },
      { key: 'backendQueued', label: 'Backend input' },
      { key: 'outputQueued', label: 'LB outputs' },
      { key: 'lbCPUQueued', label: 'LB CPU' },
      { key: 'backendCPUQueued', label: 'Selected backend CPU' },
    ],
  },
  {
    title: 'Cumulative queue drops',
    description: 'Input and CPU series count queue-full drops. LB outputs use the native all-queue-drop counter, '
      + 'including early congestion drops. Counters exclude link loss and are never summed across publications.',
    unit: 'packets',
    series: [
      { key: 'clientDrops', label: 'Client input (queue-full)' },
      { key: 'backendDrops', label: 'Backend input (queue-full)' },
      { key: 'outputDrops', label: 'LB outputs (all queue drops)' },
      { key: 'lbCPUDrops', label: 'LB CPU (queue-full)' },
      { key: 'backendCPUDrops', label: 'Selected backend CPU (queue-full)' },
    ],
  },
  {
    title: 'CPU utilization (host lifetime mean)',
    description: 'Mean modeled host utilization since host creation, including changes to available core time. '
      + 'Missing CPU measurements remain unavailable.',
    unit: '%',
    series: [
      { key: 'lbCPU', label: 'LB host mean' },
      { key: 'backendCPU', label: 'Selected backend host mean' },
    ],
  },
  {
    title: 'Reported backend connection lifetime p95',
    description: 'Native p95 reported by the selected backend for connection lifetime. '
      + 'A reported zero is retained. Global transfer latency p95 / p99 are unavailable.',
    unit: 'ms',
    series: [{ key: 'backendP95', label: 'Selected backend connection p95' }],
  },
];

function approximate(value: Numeric): number | null {
  if (value === undefined) return null;
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric >= 0 ? numeric : null;
}

function exactCounter(value: Numeric): bigint | null {
  if (value === undefined || typeof value === 'number' && !Number.isSafeInteger(value)) return null;
  if (!/^\d+$/.test(String(value))) return null;
  return BigInt(value);
}

function hostUtilization(node: Node | undefined): number | null {
  const utilization = approximate(node?.metrics.hostUtilization);
  return utilization === null ? null : approximate(utilization * 100);
}

function observedNodes(sample: CollectedSample, definition: Definition | undefined, backendId: string | null) {
  const lbId = definition?.nodes.find(node => node.kind === 'loadBalancer')?.id;
  const backendNodeId = definition?.nodes.find(node => (
    node.kind === 'backend' && node.resourceId === backendId
  ))?.id;
  return {
    lb: sample.snapshot.topology.nodes.find(node => node.id === lbId),
    backend: sample.snapshot.topology.nodes.find(node => node.id === backendNodeId),
  };
}

function inputCounter(
  sample: CollectedSample, definition: Definition | undefined,
  side: 'clients' | 'backends', counter: 'queuedPackets' | 'queueDroppedPackets',
): number | null {
  const queues = definition?.ingress?.queues.filter(queue => queue.side === side);
  if (!queues?.length) return null;
  let total = 0n;
  for (const queue of queues) {
    const state = sample.snapshot.topology.ingress?.queues.find(value => value.id === queue.id);
    const value = exactCounter(state?.stats[counter]);
    if (value === null) return null;
    const congestion = counter === 'queueDroppedPackets' ? exactCounter(state?.stats.congestionDroppedPackets ?? 0) : 0n;
    if (congestion === null || congestion > value) return null;
    total += value - congestion;
  }
  return approximate(total.toString());
}

// Resource choices are compact data; the selector renders a bounded set of matches.
export function chartBackends(result: RunResult): readonly ChartBackend[] {
  const definitions = new Map(result.collection.definitions.map(value => [value.revision, value]));
  const backends = new Map<string, ChartBackend>();
  for (const sample of orderedSamples(result)) {
    const names = new Map(definitions.get(sample.snapshot.topology.revision)?.servers.map(value => [value.id, value.name]));
    for (const server of sample.snapshot.servers) {
      backends.set(server.id, { id: server.id, label: names.get(server.id) || server.id });
    }
  }
  return [...backends.values()];
}

export function filterChartBackends(backends: readonly ChartBackend[], query: string): ChartBackend[] {
  const needle = query.toLocaleLowerCase();
  const matches: ChartBackend[] = [];
  for (const backend of backends) {
    if (`${backend.label} ${backend.id}`.toLocaleLowerCase().includes(needle)) matches.push(backend);
    if (matches.length === 20) break;
  }
  return matches;
}

function orderedSamples(result: RunResult): CollectedSample[] {
  return [...result.collection.samples].sort((left, right) => {
    const a = BigInt(left.publicationSequence);
    const b = BigInt(right.publicationSequence);
    return a < b ? -1 : a > b ? 1 : 0;
  });
}

function ackGoodput(previous: CollectedSample | undefined, sample: CollectedSample, continuous: boolean) {
  const interval = previous ? sample.virtualTimeSeconds - previous.virtualTimeSeconds : 0;
  const before = exactCounter(previous?.snapshot.simulation.payload?.acknowledgedBytes);
  const after = exactCounter(sample.snapshot.simulation.payload?.acknowledgedBytes);
  if (!continuous || interval <= 0 || before === null || after === null || after < before) {
    return { rate: null, interval: null };
  }
  // Subtract exact decimal counters before converting the delta for plotting.
  return { rate: approximate(Number(after - before) / interval), interval };
}

export function buildResultChartSamples(result: RunResult, backendId: string | null): readonly ChartSample[] {
  const definitions = new Map(result.collection.definitions.map(value => [value.revision, value]));
  const interruptions = result.collection.markers.filter(marker => marker.kind === 'disconnected');
  const samples = orderedSamples(result);
  const epoch = (sample: CollectedSample) => interruptions.filter(marker => marker.receivedAt <= sample.receivedAt).length;

  return samples.map((sample, offset) => {
    const previous = samples[offset - 1];
    const notes: string[] = [];
    if (previous && BigInt(sample.publicationSequence) !== BigInt(previous.publicationSequence) + 1n) {
      notes.push('Missing retained publications before this sample.');
    }
    if (previous && epoch(sample) !== epoch(previous)) notes.push('Connection interruption between sample receipts.');
    if (previous && sample.virtualTimeSeconds < previous.virtualTimeSeconds) notes.push('Virtual time moved backwards.');
    const breakBefore = notes.length > 0;
    const definition = definitions.get(sample.snapshot.topology.revision);
    if (!definition) notes.push('Topology definition unavailable for this revision.');
    if (previous && previous.snapshot.topology.revision !== sample.snapshot.topology.revision) {
      notes.push('Topology revision changed.');
    }
    const { lb, backend } = observedNodes(sample, definition, backendId);
    const wireWindow = approximate(lb?.traffic?.sampleWindowSeconds);
    const wireMeasured = wireWindow !== null && wireWindow > 0;
    const ack = ackGoodput(previous, sample, previous !== undefined && !breakBefore);
    const server = sample.snapshot.servers.find(value => value.id === backendId);

    return {
      index: sample.index, publicationSequence: sample.publicationSequence,
      virtualTimeSeconds: sample.virtualTimeSeconds, topologyRevision: sample.snapshot.topology.revision,
      breakBefore, notes, wireWindowSeconds: wireWindow, ackIntervalSeconds: ack.interval,
      values: {
        wireRX: wireMeasured ? approximate(lb?.traffic?.receiveBytesPerSecond) : null,
        wireTX: wireMeasured ? approximate(lb?.traffic?.transmitBytesPerSecond) : null,
        ackGoodput: ack.rate,
        clientQueued: inputCounter(sample, definition, 'clients', 'queuedPackets'),
        backendQueued: inputCounter(sample, definition, 'backends', 'queuedPackets'),
        outputQueued: approximate(lb?.metrics.forwardingQueuedPackets),
        lbCPUQueued: approximate(lb?.metrics.hostQueuedPackets),
        backendCPUQueued: approximate(backend?.metrics.hostQueuedPackets),
        clientDrops: inputCounter(sample, definition, 'clients', 'queueDroppedPackets'),
        backendDrops: inputCounter(sample, definition, 'backends', 'queueDroppedPackets'),
        outputDrops: approximate(lb?.metrics.forwardingQueueDroppedPackets),
        lbCPUDrops: approximate(lb?.metrics.hostQueueDroppedPackets),
        backendCPUDrops: approximate(backend?.metrics.hostQueueDroppedPackets),
        lbCPU: hostUtilization(lb), backendCPU: hostUtilization(backend),
        backendP95: approximate(server?.latencyP95Milli),
      },
    };
  });
}

function segments(samples: readonly ChartSample[], key: string): ChartVertex[][] {
  const output: ChartVertex[][] = [];
  let current: ChartVertex[] = [];
  for (const sample of samples) {
    const value = sample.values[key];
    if (sample.breakBefore || value === null || value === undefined) {
      if (current.length) output.push(current);
      current = [];
    }
    if (value !== null && value !== undefined) current.push({ sample, value });
  }
  if (current.length) output.push(current);
  return output;
}

function reduceSegment(points: readonly ChartVertex[], limit: number): readonly ChartVertex[] {
  if (points.length <= limit) return points;
  if (limit === 1) return [points[points.length - 1]];
  const selected = new Set([0, points.length - 1]);
  if (limit === 3) {
    let peak = 1;
    for (let index = 2; index < points.length - 1; index++) {
      if (points[index].value > points[peak].value) peak = index;
    }
    selected.add(peak);
  }
  const buckets = Math.floor((limit - 2) / 2);
  for (let bucket = 0; bucket < buckets; bucket++) {
    const start = 1 + Math.floor(bucket * (points.length - 2) / buckets);
    const end = 1 + Math.floor((bucket + 1) * (points.length - 2) / buckets);
    let minimum = start;
    let maximum = start;
    for (let index = start + 1; index < end; index++) {
      if (points[index].value < points[minimum].value) minimum = index;
      if (points[index].value > points[maximum].value) maximum = index;
    }
    selected.add(minimum);
    selected.add(maximum);
  }
  return [...selected].sort((a, b) => a - b).map(index => points[index]);
}

function pointAllocations(counts: readonly number[], budget: number): number[] {
  const total = counts.reduce((sum, count) => sum + count, 0);
  if (total <= budget) return [...counts];
  const allocations = counts.map(() => 0);
  const measured = counts.flatMap((count, index) => count > 0 ? [index] : []);
  if (budget < measured.length) {
    for (let slot = 0; slot < budget; slot++) {
      // Keep both ends when possible; a single slot keeps the last measured entry.
      const position = budget === 1 ? measured.length - 1 : Math.round(slot * (measured.length - 1) / (budget - 1));
      allocations[measured[position]] = 1;
    }
    return allocations;
  }
  const remaining = budget - measured.length;
  const extraPoints = total - measured.length;
  for (const index of measured) {
    allocations[index] = 1 + Math.floor(remaining * (counts[index] - 1) / extraPoints);
  }
  let unallocated = budget - allocations.reduce((sum, count) => sum + count, 0);
  for (const index of measured) {
    if (unallocated > 0 && allocations[index] < counts[index]) {
      allocations[index]++;
      unallocated--;
    }
  }
  return allocations;
}

// Allocate by series, then by continuous segment; omitted gaps are never bridged.
export function reduceChartTraces(samples: readonly ChartSample[], series: readonly ChartSeries[], pointBudget = 1200) {
  const budget = Math.max(1, Math.floor(pointBudget));
  const traces = series.map(value => ({ key: value.key, segments: segments(samples, value.key) }));
  const counts = traces.map(trace => trace.segments.reduce((sum, segment) => sum + segment.length, 0));
  const observedPoints = counts.reduce((sum, count) => sum + count, 0);
  const measuredSeriesKeys = traces.filter((_, index) => counts[index] > 0).map(trace => trace.key);
  if (observedPoints <= budget) return { traces, observedPoints, displayedPoints: observedPoints, measuredSeriesKeys };
  const seriesBudgets = pointAllocations(counts, budget);
  const output: ChartTrace[] = traces.map((trace, index) => {
    const limits = pointAllocations(trace.segments.map(segment => segment.length), seriesBudgets[index]);
    return {
      key: trace.key,
      segments: trace.segments.flatMap((segment, segmentIndex) => (
        limits[segmentIndex] > 0 ? [reduceSegment(segment, limits[segmentIndex])] : []
      )),
    };
  });
  const displayedPoints = output.reduce((sum, trace) => sum + trace.segments.reduce((n, segment) => n + segment.length, 0), 0);
  return { traces: output, observedPoints, displayedPoints, measuredSeriesKeys };
}
