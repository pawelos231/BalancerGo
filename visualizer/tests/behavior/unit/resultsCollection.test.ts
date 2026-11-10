import assert from 'node:assert/strict';
import test from 'node:test';
import {
  COLLECTION_BYTE_BUDGET,
  COLLECTION_TERMINAL_RESERVE,
  ResultsCollection,
} from '../../../src/results/collection.ts';
import { readTelemetryJSON, telemetrySource } from '../../../src/telemetry/exact.ts';
import type { SimulationSnapshot, TopologyDefinition } from '../../../src/telemetry/types.ts';
import { idleFrame, telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

test('raw collection keeps every unique receipt, equal virtual times and arrival order', () => {
  const collection = new ResultsCollection();
  assert.equal(collection.snapshot(idleFrame(), 1), false);
  assert.equal(collection.usage.runCount, 0);
  for (const [sequence, time] of [['1', 100], ['3', 101], ['2', 102]] as const) {
    assert.equal(collection.snapshot(telemetryFrame('1', 'run-1', 1, sequence), time), true);
  }
  assert.equal(collection.snapshot(telemetryFrame('1', 'run-1', 1, '3'), 103), false);
  assert.equal(collection.snapshot(telemetryFrame('1', 'run-2', 1, '1'), 104), true);
  const run = collection.get('run-1')!;
  assert.equal(run.collectionVersion, 1);
  assert.deepEqual(run.samples.map((sample) => sample.index), [1, 2, 3]);
  assert.deepEqual(run.samples.map((sample) => sample.publicationSequence), ['1', '3', '2']);
  assert.deepEqual(run.samples.map((sample) => sample.virtualTimeSeconds), [0.1, 0.1, 0.1]);
  assert.deepEqual(run.samples.map((sample) => sample.receivedAt), [100, 101, 102]);
  assert.equal(run.firstObservedSequence, '1');
  assert.equal(run.highestObservedSequence, '3');
  assert.equal(collection.get('run-2')!.samples[0].index, 1);
  assert.equal(collection.get('absent'), undefined);
});

test('exact wire counters and definitions are archived before any hydration', () => {
  const frame = telemetryFrame();
  frame.simulation.payload = { receivedBytes: 42, acknowledgedBytes: 42 };
  const rawFrame = readTelemetryJSON(JSON.stringify(frame)
    .replaceAll(':42', ':18446744073709551615')) as SimulationSnapshot;
  const definition = topologyDefinition();
  definition.links[0].config.bandwidthBitsPerSecond = 43;
  const rawDefinition = readTelemetryJSON(JSON.stringify(definition)
    .replaceAll(':43', ':9007199254740993')) as TopologyDefinition;
  const collection = new ResultsCollection();

  collection.snapshot(rawFrame, 100);
  assert.equal(collection.get('run-1')!.definitions.length, 0);
  collection.definition(rawDefinition, 101);
  collection.definition(rawDefinition, 102);
  collection.definition(topologyDefinition('2'), 103);
  const run = collection.get('run-1')!;
  assert.equal(run.samples[0].snapshot.simulation.payload?.receivedBytes, '18446744073709551615');
  assert.equal(run.definitions[0].links[0].config.bandwidthBitsPerSecond, '9007199254740993');
  assert.deepEqual(run.definitions.map((entry) => entry.revision), ['1', '2']);
});

test('collection owns immutable copies of both registered sources and local inputs', () => {
  const frame = telemetryFrame();
  const parsed = readTelemetryJSON(JSON.stringify(telemetryFrame('2'))) as SimulationSnapshot;
  const definition = topologyDefinition();
  const collection = new ResultsCollection();
  collection.snapshot(frame, 100);
  collection.snapshot(parsed, 101); // Same publication is ignored.
  const parsedUnique = readTelemetryJSON(
    JSON.stringify(telemetryFrame('2', 'run-1', 2, '2')),
  ) as SimulationSnapshot;
  collection.snapshot(parsedUnique, 102);
  collection.definition(definition, 103);
  frame.servers[0].numberOfHandledPackets = 999;
  definition.nodes[0].label = 'Mutated';
  telemetrySource(parsedUnique).servers[0].numberOfHandledPackets = '999999999999999999';
  parsedUnique.servers[0].clients.length = 0;

  const run = collection.get('run-1')!;
  assert.equal(run.samples[0].snapshot.servers[0].numberOfHandledPackets, 1);
  assert.equal(run.samples[1].snapshot.servers[0].numberOfHandledPackets, 2);
  assert.equal(run.samples[1].snapshot.servers[0].clients.length, 1);
  assert.equal(run.definitions[0].nodes[0].label, 'LB');
  assert.ok(Object.isFrozen(run));
  assert.ok(Object.isFrozen(run.samples));
  assert.ok(Object.isFrozen(run.samples[0].snapshot.servers[0]));
  collection.snapshot(telemetryFrame('2', 'run-1', 3, '3'), 104);
  assert.equal(run.samples.length, 2); // Previously read views stay stable.
  assert.equal(collection.get('run-1')!.samples.length, 3);
});

test('late attachment, publication gaps and connection interruptions stay explicit', () => {
  const collection = new ResultsCollection();
  collection.snapshot(telemetryFrame('1', 'run-1', 1, '9007199254740993'), 100);
  collection.disconnected(101);
  collection.disconnected(102);
  collection.reconnected(103);
  collection.reconnected(104);
  collection.snapshot(telemetryFrame('1', 'run-1', 2, '9007199254740996'), 105);
  assert.deepEqual(collection.get('run-1')!.markers, [
    { kind: 'lateJoin', publicationSequence: '9007199254740993', receivedAt: 100 },
    { kind: 'disconnected', receivedAt: 101 },
    { kind: 'reconnected', receivedAt: 103 },
    { kind: 'gap', afterSequence: '9007199254740993', beforeSequence: '9007199254740996', receivedAt: 105 },
  ]);
});

test('terminal duplicate is kept once and its own definition can arrive afterwards', () => {
  const collection = new ResultsCollection();
  const frame = telemetryFrame('2');
  collection.snapshot(frame, 100);
  assert.equal(collection.snapshot(frame, 101, true), false);
  assert.equal(collection.definition(topologyDefinition('2'), 101, true), true);
  const run = collection.get('run-1')!;
  assert.equal(run.samples.length, 1);
  assert.equal(run.samples[0].receivedAt, 100);
  assert.equal(run.terminalPublicationSequence, '1');
  assert.equal(run.limits.terminalSnapshotDropped, false);
  assert.equal(run.definitions[0].revision, '2');
});

test('late terminal data from A cannot move connection markers away from live B', () => {
  const collection = new ResultsCollection();
  collection.snapshot(telemetryFrame('1', 'A'), 100);
  collection.snapshot(telemetryFrame('1', 'B'), 101);
  collection.snapshot(telemetryFrame('1', 'A'), 102, true);
  collection.disconnected(103);
  collection.reconnected(104);
  assert.deepEqual(collection.get('A')!.markers, []);
  assert.deepEqual(collection.get('B')!.markers, [
    { kind: 'disconnected', receivedAt: 103 },
    { kind: 'reconnected', receivedAt: 104 },
  ]);
  collection.snapshot(telemetryFrame('1', 'B'), 105, true);
  collection.disconnected(106);
  assert.equal(collection.get('B')!.markers.length, 2);
});

test('ordinary deliveries from ended A cannot move connection markers away from B', () => {
  const collection = new ResultsCollection();
  collection.snapshot(telemetryFrame('1', 'A'), 100);
  collection.snapshot(telemetryFrame('1', 'A', 2, '2'), 101, true);
  collection.snapshot(telemetryFrame('1', 'B'), 102);
  assert.equal(collection.snapshot(telemetryFrame('1', 'A'), 103), false);
  assert.equal(collection.snapshot(telemetryFrame('1', 'A', 3, '3'), 104), true);
  collection.disconnected(105);
  collection.reconnected(106);
  assert.deepEqual(collection.get('A')!.markers, []);
  assert.deepEqual(collection.get('B')!.markers, [
    { kind: 'disconnected', receivedAt: 105 },
    { kind: 'reconnected', receivedAt: 106 },
  ]);
  assert.deepEqual(collection.get('A')!.samples.map((sample) => sample.publicationSequence),
    ['1', '2', '3']);
});

test('unique out-of-order and duplicate deliveries do not select a different active run', () => {
  const collection = new ResultsCollection();
  collection.snapshot(telemetryFrame('1', 'A'), 100);
  collection.snapshot(telemetryFrame('1', 'A', 3, '3'), 101);
  collection.snapshot(telemetryFrame('1', 'B'), 102);
  assert.equal(collection.snapshot(telemetryFrame('1', 'A', 3, '3'), 103), false);
  assert.equal(collection.snapshot(telemetryFrame('1', 'A', 2, '2'), 104), true);
  collection.disconnected(105);
  assert.equal(collection.get('A')!.markers.filter((marker) => marker.kind === 'disconnected').length, 0);
  assert.deepEqual(collection.get('B')!.markers, [{ kind: 'disconnected', receivedAt: 105 }]);
});

test('aggregate serialized budget preserves a terminal reserve and reports each kind of loss', () => {
  const collection = new ResultsCollection({ byteBudget: 16_000, terminalReserveBytes: 8_000 });
  assert.equal(collection.snapshot(telemetryFrame(), 100), true);
  assert.equal(collection.snapshot(telemetryFrame('1', 'run-1', 2, '2'), 101), false);
  const tooLarge = topologyDefinition();
  tooLarge.nodes[0].label = 'é'.repeat(10_000);
  assert.equal(collection.definition(tooLarge, 102), false);
  assert.equal(collection.snapshot(telemetryFrame('1', 'run-1', 3, '3'), 103, true), true);
  assert.equal(collection.definition(topologyDefinition(), 104, true), true);
  const run = collection.get('run-1')!;
  assert.deepEqual(run.samples.map((sample) => sample.publicationSequence), ['1', '3']);
  assert.equal(run.limits.memoryLimited, true);
  assert.equal(run.limits.droppedSamples, 1);
  assert.equal(run.limits.droppedDefinitions, 1);
  assert.equal(run.limits.terminalSnapshotDropped, false);
  assert.equal(collection.usage.memoryLimited, true);
  assert.ok(collection.usage.serializedBytes <= collection.usage.byteBudget);
  assert.ok(new TextEncoder().encode(JSON.stringify(run)).byteLength < collection.usage.serializedBytes);
});

test('terminal sample larger than the hard budget is explicitly rejected', () => {
  const collection = new ResultsCollection({ byteBudget: 8_000, terminalReserveBytes: 4_000 });
  const frame = telemetryFrame();
  frame.topology.nodes[0].tcpPhase = 'x'.repeat(10_000);
  assert.equal(collection.snapshot(frame, 100, true), false);
  const run = collection.get('run-1')!;
  assert.equal(run.limits.terminalSnapshotDropped, true);
  assert.equal(run.limits.droppedSamples, 1);
  assert.equal(run.terminalPublicationSequence, '1');
  assert.equal(run.samples.length, 0);
  assert.ok(collection.usage.serializedBytes <= 8_000);
});

test('run and marker metadata have separate fixed caps and release returns aggregate capacity', () => {
  const collection = new ResultsCollection({ maxRuns: 2, maxMarkersPerRun: 2 });
  collection.snapshot(telemetryFrame(), 100);
  collection.snapshot(telemetryFrame('1', 'run-2'), 101);
  const beforeRejection = collection.usage.serializedBytes;
  for (let index = 0; index < 100; index++) {
    assert.equal(collection.snapshot(telemetryFrame('1', `rejected-${index}`), 102), false);
  }
  assert.equal(collection.usage.runCount, 2);
  assert.equal(collection.usage.runLimitReached, true);
  assert.equal(collection.usage.droppedRunRecords, 100);
  assert.equal(collection.usage.serializedBytes, beforeRejection);
  for (let index = 0; index < 5; index++) {
    collection.disconnected(103);
    collection.reconnected(104);
  }
  assert.equal(collection.get('run-1')!.markers.length, 0);
  assert.equal(collection.get('run-2')!.markers.length, 2);
  assert.equal(collection.get('run-2')!.limits.markerLimitReached, true);
  assert.equal(collection.get('run-2')!.limits.droppedMarkers, 8);
  collection.release('run-2');
  collection.release('run-2');
  assert.equal(collection.get('run-2'), undefined);
  assert.ok(collection.usage.serializedBytes < beforeRejection);
  assert.equal(collection.snapshot(telemetryFrame('1', 'run-3'), 105), true);
});

test('UTF-8 definitions and run identities consume the same aggregate budget as samples', () => {
  const collection = new ResultsCollection({ byteBudget: 10_000, terminalReserveBytes: 0 });
  const definition = topologyDefinition('1', 'unicode');
  definition.nodes[0].label = '😀'.repeat(1_000);
  assert.equal(collection.definition(definition, 100), true);
  assert.equal(collection.snapshot(telemetryFrame('1', 'other'), 101), false);
  assert.equal(collection.get('other'), undefined);
  assert.equal(collection.usage.memoryLimited, true);
  assert.ok(collection.usage.serializedBytes <= 10_000);
  const oversizedRun = telemetryFrame('1', 'ą'.repeat(10_000));
  assert.equal(collection.snapshot(oversizedRun, 102), false);
  assert.equal(collection.usage.runCount, 1);
  assert.ok(new TextEncoder().encode(JSON.stringify(collection.get('unicode'))).byteLength
    < collection.usage.serializedBytes);
});

test('defaults state the retained serialized budget and invalid limits fail early', () => {
  const usage = new ResultsCollection().usage;
  assert.equal(usage.byteBudget, COLLECTION_BYTE_BUDGET);
  assert.equal(usage.terminalReserveBytes, COLLECTION_TERMINAL_RESERVE);
  for (const options of [
    { byteBudget: -1 }, { terminalReserveBytes: -1 }, { maxRuns: 0 },
    { maxMarkersPerRun: -1 }, { byteBudget: Infinity },
  ]) assert.throws(() => new ResultsCollection(options), /limits/);
  const collection = new ResultsCollection();
  assert.throws(() => collection.snapshot(telemetryFrame(), NaN), /timestamp/);
  assert.throws(() => collection.definition(topologyDefinition(), Infinity), /timestamp/);
});
