import assert from 'node:assert/strict';
import test from 'node:test';
import {
  buildResultChartSamples, chartBackends, filterChartBackends, reduceChartTraces, resultChartDefinitions,
} from '../../../src/results/chartData.ts';
import type { ChartSample } from '../../../src/results/chartData.ts';
import type { ExactTelemetry } from '../../../src/telemetry/exact.ts';
import type { SimulationSnapshot, TopologyDefinition } from '../../../src/telemetry/types.ts';
import type { CollectedSample, ObservationMarker, RunResult } from '../../../src/results/types.ts';
import { resultFixture } from '../../resultFixture.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

type Frame = ExactTelemetry<SimulationSnapshot>;

function definition(revision = '1'): TopologyDefinition {
  const value = topologyDefinition(revision);
  value.nodes[0].id = 'physical-router';
  value.nodes[0].resourceId = 'scenario-router';
  value.nodes[1].id = 'physical-worker';
  value.nodes[1].resourceId = 'srv';
  return value;
}

function frame(sequence: string, time: number, ack: number | string = 0, revision = '1'): Frame {
  const value: Frame = telemetryFrame(revision, 'run-1', 1, sequence);
  value.simulation.virtualTimeSeconds = time;
  value.simulation.payload = { receivedBytes: ack, acknowledgedBytes: ack };
  value.topology.nodes[0].id = 'physical-router';
  value.topology.nodes[1].id = 'physical-worker';
  return value;
}

function result(frames: readonly Frame[], definitions: readonly TopologyDefinition[] = [definition()],
  markers: readonly ObservationMarker[] = []): RunResult {
  const base = resultFixture();
  const samples: CollectedSample[] = frames.map((snapshot, index) => ({
    snapshot, index, publicationSequence: snapshot.simulation.publicationSequence!,
    virtualTimeSeconds: Number(snapshot.simulation.virtualTimeSeconds), receivedAt: index * 10,
  }));
  return { ...base, collection: { ...base.collection, samples, definitions, markers } };
}

test('charts sort exact publication IDs, retain equal timestamps and subtract ACK bytes before approximation', () => {
  const source = result([
    frame('9007199254740995', 4, '18446744073709551615'),
    frame('9007199254740993', 2, '18446744073709551610'),
    frame('9007199254740994', 2, '18446744073709551612'),
  ]);
  const before = JSON.stringify(source);
  const samples = buildResultChartSamples(source, 'srv');
  assert.deepEqual(samples.map(sample => sample.publicationSequence), [
    '9007199254740993', '9007199254740994', '9007199254740995',
  ]);
  assert.deepEqual(samples.map(sample => sample.virtualTimeSeconds), [2, 2, 4]);
  assert.deepEqual(samples.map(sample => sample.values.ackGoodput), [null, null, 1.5]);
  assert.equal(samples[1].ackIntervalSeconds, null);
  assert.equal(samples[2].ackIntervalSeconds, 2);
  assert.equal(JSON.stringify(source), before);
});

test('goodput is unavailable across missing publications, interruption epochs, absent payload and decreasing counters', () => {
  const frames = [frame('1', 0, 0), frame('3', 1, 10), frame('4', 2, 20), frame('5', 3, 19), frame('6', 4, 30)];
  delete frames[4].simulation.payload;
  const samples = buildResultChartSamples(result(frames, [definition()], [
    { kind: 'disconnected', receivedAt: 15 }, { kind: 'reconnected', receivedAt: 18 },
  ]), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.ackGoodput), [null, null, null, null, null]);
  assert.equal(samples[1].breakBefore, true);
  assert.equal(samples[2].breakBefore, true);
  assert.match(samples[1].notes.join(' '), /Missing retained publications/);
  assert.match(samples[2].notes.join(' '), /Connection interruption/);
});

test('historical gap markers do not invent a remaining gap after out-of-order publications fill it', () => {
  const samples = buildResultChartSamples(result([
    frame('3', 2, 6), frame('1', 0, 0), frame('2', 1, 2),
  ], [definition()], [{ kind: 'gap', afterSequence: '1', beforeSequence: '3', receivedAt: 0 }]), 'srv');
  assert.deepEqual(samples.map(sample => sample.breakBefore), [false, false, false]);
  assert.deepEqual(samples.map(sample => sample.values.ackGoodput), [null, 2, 4]);
});

test('wire rates use the matched semantic LB definition and require a positive native measurement window', () => {
  const first = frame('1', 0);
  first.topology.nodes[0].traffic = { receiveBytesPerSecond: 40, transmitBytesPerSecond: 70, sampleWindowSeconds: 0 };
  const second = frame('2', 1);
  second.topology.nodes[0].traffic = { receiveBytesPerSecond: 50, transmitBytesPerSecond: 80, sampleWindowSeconds: 0.75 };
  const missing = frame('3', 2, 0, 'missing-definition');
  const samples = buildResultChartSamples(result([first, second, missing]), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.wireRX), [null, 50, null]);
  assert.deepEqual(samples.map(sample => sample.values.wireTX), [null, 80, null]);
  assert.match(samples[2].notes.join(' '), /Topology definition unavailable/);
});

test('host mean matches backend resourceId, preserves disabled-host history and never invents absent CPU values', () => {
  const frames = [frame('1', 0), frame('2', 1), frame('3', 2)];
  frames[0].topology.nodes[0].metrics = { hostUtilization: 0.25 };
  frames[0].topology.nodes[1].metrics = { hostUtilization: 0, hostQueuedPackets: 0 };
  frames[1].topology.nodes[0].metrics = { hostUtilization: 0.5 };
  frames[1].topology.nodes[1].metrics = { hostUtilization: 0.75 };
  frames[2].servers[0].cpuUsage = 0.9;
  const definitions = [definition()];
  definitions[0].nodes[1].metrics = [{ key: 'hostCPUEnabled', label: 'CPU enabled', value: 0 }];
  const samples = buildResultChartSamples(result(frames, definitions), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.lbCPU), [25, 50, null]);
  assert.deepEqual(samples.map(sample => sample.values.backendCPU), [0, 75, null]);
  assert.deepEqual(samples.map(sample => sample.values.backendCPUQueued), [0, null, null]);
  assert.deepEqual(buildResultChartSamples(result(frames, definitions), 'missing').map(sample => sample.values.backendCPU),
    [null, null, null]);
});

test('backend p95 retains native zero, disappears with the resource and is explicitly scoped to connection lifetime', () => {
  const frames = [frame('1', 0), frame('2', 1), frame('3', 2)];
  frames[0].servers[0].latencyP95Milli = 0;
  frames[1].servers = [];
  frames[2].servers[0].latencyP95Milli = 12;
  const samples = buildResultChartSamples(result(frames, []), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.backendP95), [0, null, 12]);
  const chart = resultChartDefinitions.find(value => value.series[0].key === 'backendP95')!;
  assert.match(chart.title, /connection lifetime p95/);
  assert.match(chart.description, /Global transfer latency p95 \/ p99 are unavailable/);
});

test('queue-full counts subtract exact early congestion drops, do not add publications and reject missing queue states', () => {
  const first = frame('1', 0);
  first.topology.ingress!.queues[0].stats.queueDroppedPackets = '18446744073709551615';
  first.topology.ingress!.queues[0].stats.congestionDroppedPackets = '18446744073709551612';
  first.topology.nodes[0].metrics = { forwardingQueueDroppedPackets: 15, hostQueueDroppedPackets: 2 };
  first.topology.nodes[1].metrics = { hostQueueDroppedPackets: 1 };
  const second = frame('2', 1);
  second.topology.ingress!.queues[0].stats.queueDroppedPackets = 8;
  second.topology.ingress!.queues[0].stats.congestionDroppedPackets = 2;
  const third = frame('3', 2);
  third.topology.ingress!.queues = [];
  const samples = buildResultChartSamples(result([first, second, third]), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.clientDrops), [3, 6, null]);
  assert.equal(samples[0].values.outputDrops, 15);
  assert.equal(samples[0].values.lbCPUDrops, 2);
  assert.equal(samples[0].values.backendCPUDrops, 1);
  assert.equal(samples[2].values.clientQueued, null);
  assert.match(resultChartDefinitions.find(value => value.title === 'Cumulative queue drops')!.description,
    /including early congestion drops/);
});

test('revision-specific definitions resolve changing physical IDs and missing resource intervals split traces', () => {
  const later = definition('2');
  later.nodes[0].id = 'replacement-router-node';
  later.nodes[1].id = 'replacement-worker-node';
  const frames = [frame('1', 0), frame('2', 1), frame('3', 2, 0, '2')];
  frames[1].topology.nodes = [];
  frames[2].topology.nodes[0].id = later.nodes[0].id;
  frames[2].topology.nodes[1].id = later.nodes[1].id;
  frames[0].topology.nodes[1].metrics = { hostUtilization: 0.2 };
  frames[2].topology.nodes[1].metrics = { hostUtilization: 0.6 };
  const samples = buildResultChartSamples(result(frames, [definition(), later]), 'srv');
  assert.deepEqual(samples.map(sample => sample.values.backendCPU), [20, null, 60]);
  assert.match(samples[2].notes.join(' '), /Topology revision changed/);
  const reduced = reduceChartTraces(samples, [{ key: 'backendCPU', label: 'CPU' }]);
  assert.deepEqual(reduced.traces[0].segments.map(segment => segment.length), [1, 1]);
});

function plotSample(index: number, value: number | null, breakBefore = false): ChartSample {
  return {
    index, publicationSequence: String(index + 1), virtualTimeSeconds: index,
    topologyRevision: '1', breakBefore, notes: [], wireWindowSeconds: null, ackIntervalSeconds: null,
    values: { value },
  };
}

test('bounded display reduction keeps bucket extrema and never joins through omitted missing values or gaps', () => {
  const source = Array.from({ length: 10_000 }, (_, index) => plotSample(index,
    index === 2000 ? 999 : index === 4000 ? null : index % 10, index === 7000));
  const before = JSON.stringify(source);
  const reduced = reduceChartTraces(source, [{ key: 'value', label: 'Value' }], 1000);
  assert.ok(reduced.displayedPoints <= 1000);
  assert.equal(reduced.observedPoints, 9999);
  const segments = reduced.traces[0].segments;
  assert.equal(segments.length, 3);
  assert.ok(segments.some(segment => segment.some(vertex => vertex.value === 999)));
  assert.ok(segments.every(segment => !(segment[0].sample.index < 4000 && segment[segment.length - 1].sample.index > 4000)));
  assert.ok(segments.every(segment => !(segment[0].sample.index < 7000 && segment[segment.length - 1].sample.index >= 7000)));
  assert.equal(JSON.stringify(source), before);
});

test('display budget also bounds highly fragmented histories, empty records, singleton and zero flatlines', () => {
  const series = [{ key: 'value', label: 'Value' }];
  const fragmented = Array.from({ length: 10_000 }, (_, index) => plotSample(index, index % 2 ? null : index));
  const reduced = reduceChartTraces(fragmented, series, 1000);
  assert.equal(reduced.displayedPoints, 1000);
  assert.ok(reduced.traces[0].segments.every(segment => segment.length === 1));
  assert.deepEqual(reduceChartTraces([], series).traces[0].segments, []);
  assert.equal(reduceChartTraces([plotSample(0, 0)], series).traces[0].segments[0][0].value, 0);
  assert.deepEqual(buildResultChartSamples(result([]), null), []);
});

test('fragmented histories preserve each measured series and raw availability even with a tiny display budget', () => {
  const series = [{ key: 'value', label: 'Fragmented' }, { key: 'rare', label: 'Rare' }];
  const samples = Array.from({ length: 2400 }, (_, index) => ({
    ...plotSample(index, index % 2 === 0 ? index : null),
    values: { value: index % 2 === 0 ? index : null, rare: index === 2399 ? 42 : null },
  }));
  const reduced = reduceChartTraces(samples, series);
  assert.equal(reduced.observedPoints, 1201);
  assert.equal(reduced.displayedPoints, 1200);
  assert.deepEqual(reduced.measuredSeriesKeys, ['value', 'rare']);
  assert.ok(reduced.traces[0].segments.every(segment => segment.length === 1));
  assert.equal(reduced.traces[0].segments[0][0].sample.index, 0);
  assert.equal(reduced.traces[0].segments.at(-1)![0].sample.index, 2398);
  assert.equal(reduced.traces[1].segments[0][0].value, 42);
  const tiny = reduceChartTraces(samples, series, 1);
  assert.equal(tiny.displayedPoints, 1);
  assert.equal(tiny.traces[0].segments.length, 0);
  assert.deepEqual(tiny.measuredSeriesKeys, ['value', 'rare']);
});

test('backend choices follow publication order and filtering bounds DOM candidates for ten thousand backends', () => {
  const early = frame('1', 0);
  const late = frame('2', 1);
  early.servers[0].id = 'first-worker';
  late.servers[0].id = 'last-worker';
  const choices = chartBackends(result([late, early]));
  assert.deepEqual(choices.map(value => value.id), ['first-worker', 'last-worker']);
  const many = Array.from({ length: 10_000 }, (_, index) => ({ id: `worker-${index}`, label: `Worker ${index}` }));
  assert.equal(filterChartBackends(many, '').length, 20);
  assert.deepEqual(filterChartBackends(many, 'worker-9999'), [many[9999]]);
  assert.deepEqual(filterChartBackends(many, 'no-match'), []);
  assert.deepEqual(chartBackends(result([])), []);
});
