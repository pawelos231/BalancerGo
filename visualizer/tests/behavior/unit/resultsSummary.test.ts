import assert from 'node:assert/strict';
import test from 'node:test';
import { assembleRunResult } from '../../../src/results/assembly.ts';
import { ResultsCollection } from '../../../src/results/collection.ts';
import type { CurrentSimulation } from '../../../src/scenarios/types.ts';
import { readTelemetryJSON, telemetrySource } from '../../../src/telemetry/exact.ts';
import type { SimulationSnapshot } from '../../../src/telemetry/types.ts';
import { experimentFixture, scenarioFixture } from '../../scenarioFixture.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

function current(runId = 'run-1', scenario = scenarioFixture()): CurrentSimulation {
  return { runId, status: 'running', scenario, experiment: experimentFixture() };
}

function assemble(collection: ResultsCollection, runId = 'run-1', reason: 'finished' | 'stopped' = 'finished') {
  return assembleRunResult(collection.get(runId)!, {
    id: `local-${runId}`, completion: { reason, receivedAt: 500 },
  });
}

function measuredFrame(value: number, sequence = String(value)) {
  const frame = telemetryFrame('1', 'run-1', value, sequence);
  frame.simulation.payload = { receivedBytes: value * 100, acknowledgedBytes: value * 80 };
  frame.simulation.performance = {
    wallTimeSeconds: 2, eventsExecuted: value * 1_000,
    eventsPerWallSecond: value * 500, forwardedPackets: value * 10,
    forwardedPacketsPerWallSecond: value * 5,
  };
  const duration = { samples: value, meanSeconds: 0.1, minSeconds: 0, maxSeconds: 0.2 };
  frame.simulation.transfers = {
    started: value, notStarted: 2, succeeded: value - 1, failed: 1, inProgress: 0,
    failures: { backendRemoved: 1 }, synRetransmissions: value, dataRetransmissions: value * 2,
    handshake: { ...duration }, payload: { ...duration }, completion: { ...duration },
  };
  return frame;
}

test('a standalone versioned result owns the real scenario, manifest, raw data and completion', () => {
  const collection = new ResultsCollection();
  const metadata = current();
  collection.metadata(metadata, 10);
  collection.definition(topologyDefinition(), 20);
  collection.snapshot(measuredFrame(1), 100);
  collection.snapshot(measuredFrame(2), 200, true);
  const result = assemble(collection);

  assert.equal(result.resultVersion, 1);
  assert.equal(result.id, 'local-run-1');
  assert.equal(result.runId, 'run-1');
  assert.deepEqual(result.completion, { reason: 'finished', receivedAt: 500 });
  assert.equal(result.context.scenarioProvenance, 'backend');
  assert.equal(result.context.seed, '-9007199254740993');
  assert.deepEqual(result.context.publicationInterval, { clock: 'real', configuredNanoseconds: 12_345_678 });
  assert.deepEqual(result.context.experiment, experimentFixture());
  assert.equal(result.context.experimentProvenance, 'backend');
  assert.equal(result.collection.samples.length, 2);
  assert.equal(result.collection.definitions.length, 1);
  assert.equal(result.coverage.complete, true);
  assert.equal(result.summary.basis, 'terminal');
  assert.deepEqual(JSON.parse(JSON.stringify(result)), result);

  metadata.scenario.fixed.execution.seed = 0;
  metadata.experiment.build.vcsRevision = 'mutated';
  collection.release('run-1');
  assert.equal(result.context.seed, '-9007199254740993');
  assert.equal(result.context.experiment!.build.vcsRevision, 'test-revision');
  assert.ok(Object.isFrozen(result));
  assert.ok(Object.isFrozen(result.collection.samples[0].snapshot.servers[0]));
  assert.throws(() => { result.summary.resources.nodes[0].value.metrics.completedStreams = 99; }, TypeError);
});

test('cumulative summaries select the highest publication despite out-of-order receipt and equal virtual times', () => {
  const collection = new ResultsCollection();
  collection.metadata(current(), 10);
  collection.definition(topologyDefinition(), 20);
  for (const sequence of [1, 3, 2]) {
    const frame = measuredFrame(sequence);
    frame.simulation.virtualTimeSeconds = 2;
    collection.snapshot(frame, 100 + sequence, sequence === 3);
  }
  const result = assemble(collection);
  assert.deepEqual(result.collection.samples.map((sample) => sample.publicationSequence), ['1', '3', '2']);
  assert.deepEqual(result.collection.samples.map((sample) => sample.virtualTimeSeconds), [2, 2, 2]);
  assert.equal(result.summary.observation!.publicationSequence, '3');
  assert.equal(result.summary.payload!.receivedBytes, 300);
  assert.equal(result.summary.transfers!.started, 3);
  assert.equal(result.summary.transfers!.synRetransmissions, 3);
  assert.equal(result.summary.performance!.eventsExecuted, 3_000);
  assert.equal(result.summary.resources.servers[0].value.numberOfHandledPackets, 3);
  assert.equal(result.summary.resources.links[0].value.stats.deliveredBytes, 300);
  assert.equal(result.summary.payloadRate!.approximateReceivedBytesPerSecond, 150);
  assert.equal(result.summary.performance!.eventsPerWallSecond, 1_500);
  assert.equal(result.summary.transferLatencyP95Seconds, null);
  assert.equal(result.summary.transferLatencyP99Seconds, null);
  assert.deepEqual(result.coverage.unrecordedPublications, []);
  assert.equal(result.coverage.complete, true); // A late publication filled the earlier gap.
});

test('removed resources retain their last observation, presence and host lifetime CPU mean', () => {
  const collection = new ResultsCollection();
  collection.definition(topologyDefinition(), 10);
  const first = measuredFrame(1);
  first.topology.nodes[0].metrics.hostUtilization = 0.25;
  first.topology.nodes[0].metrics.hostBusyTimeSeconds = 1;
  first.topology.nodes[0].metrics.hostAvailableCoreTimeSeconds = 4;
  first.servers[0].cpuUsage = 0.4;
  collection.snapshot(first, 100);
  const last = measuredFrame(2);
  last.topology.nodes = last.topology.nodes.filter((node) => node.id === 'lb');
  last.topology.nodes[0].active = false;
  last.topology.nodes[0].metrics.hostUtilization = 0.75;
  last.servers = [];
  last.topology.links = [];
  delete last.topology.ingress;
  collection.snapshot(last, 200, true);
  const resources = assemble(collection, 'run-1', 'stopped').summary.resources;

  const backend = resources.nodes.find((node) => node.id === 'backend')!;
  assert.equal(backend.presentInSummarySample, false);
  assert.equal(backend.observation.publicationSequence, '1');
  assert.equal(backend.observation.virtualTimeSeconds, 0.1);
  assert.equal(backend.value.metrics.completedStreams, 1);
  assert.equal(backend.kind, 'backend');
  const lb = resources.nodes.find((node) => node.id === 'lb')!;
  assert.equal(lb.presentInSummarySample, true);
  assert.equal(lb.value.active, false);
  assert.equal(lb.cpu.basis, 'hostLifetimeMean');
  assert.equal(lb.cpu.utilizationRatio, 0.75);
  assert.equal(lb.cpu.busyTimeSeconds, null);
  assert.equal(resources.servers[0].cpu.utilizationRatio, 0.4);
  assert.equal(resources.servers[0].presentInSummarySample, false);
  assert.equal(resources.servers[0].value.latencyP95Milli, 1);
  assert.equal(resources.clients[0].value.serverId, 'srv');
  assert.equal(resources.clients[0].presentInSummarySample, false);
  assert.equal(resources.links[0].presentInSummarySample, false);
  assert.equal(resources.ingress!.presentInSummarySample, false);
  assert.equal(resources.queues[0].presentInSummarySample, false);
  assert.equal(resources.nodes.find((node) => node.id === 'client-a')!.groupId, 'clients');
});

test('a retained terminal publication stays authoritative if a higher ordinary frame arrives later', () => {
  const collection = new ResultsCollection();
  collection.snapshot(measuredFrame(1), 100);
  collection.snapshot(measuredFrame(2), 200, true);
  collection.snapshot(measuredFrame(3), 300);
  const result = assemble(collection);
  assert.equal(result.summary.basis, 'terminal');
  assert.equal(result.summary.observation!.publicationSequence, '2');
  assert.equal(result.summary.payload!.receivedBytes, 200);
  assert.equal(result.summary.resources.nodes[0].value.metrics.completedStreams, 2);
  assert.equal(result.collection.samples.length, 3);
});

test('exact wire counters and compact missing-publication counts never pass through Number arithmetic', () => {
  const frame = measuredFrame(1, '18446744073709551615');
  frame.simulation.payload!.receivedBytes = 42;
  frame.simulation.transfers!.started = 43;
  frame.servers[0].numberOfHandledPackets = 44;
  const wire = readTelemetryJSON(JSON.stringify(frame)
    .replace('"receivedBytes":42', '"receivedBytes":18446744073709551615')
    .replace('"started":43', '"started":9007199254740993')
    .replace('"numberOfHandledPackets":44', '"numberOfHandledPackets":9007199254740995')) as SimulationSnapshot;
  const collection = new ResultsCollection();
  collection.snapshot(wire, 100, true);
  const sourceBefore = JSON.stringify(telemetrySource(wire));
  const result = assemble(collection);

  assert.equal(result.summary.payload!.receivedBytes, '18446744073709551615');
  assert.equal(result.summary.transfers!.started, '9007199254740993');
  assert.equal(result.summary.resources.servers[0].value.numberOfHandledPackets, '9007199254740995');
  assert.deepEqual(result.coverage.unrecordedPublications, [{ first: '1', last: '18446744073709551614' }]);
  assert.equal(result.coverage.unrecordedPublicationCount, '18446744073709551614');
  assert.equal(JSON.stringify(telemetrySource(wire)), sourceBefore);
});

test('late metadata is keyed to its own run and backend metadata supersedes submitted provenance', () => {
  const collection = new ResultsCollection();
  const submitted = scenarioFixture();
  submitted.fixed.execution.seed = 11;
  collection.snapshot(measuredFrame(1), 100, true);
  collection.submitted('run-1', submitted, 110);
  const fallback = assemble(collection);
  assert.equal(fallback.context.scenarioProvenance, 'submitted');
  assert.equal(fallback.context.seed, 11);
  assert.equal(fallback.coverage.missingAuthoritativeScenario, true);
  assert.equal(fallback.coverage.metadataAfterFirstSample, true);

  const newer = scenarioFixture();
  newer.fixed.execution.seed = 22;
  collection.metadata(current('run-B', newer), 120);
  assert.equal(assemble(collection).context.seed, 11);
  const authoritative = scenarioFixture();
  authoritative.fixed.execution.seed = 33;
  collection.metadata(current('run-1', authoritative), 130);
  assert.equal(collection.submitted('run-1', submitted, 140), false);
  const completed = assemble(collection);
  assert.equal(completed.context.scenarioProvenance, 'backend');
  assert.equal(completed.context.seed, 33);
  assert.equal(completed.context.scenarioReceivedAt, 130);
  assert.equal(completed.coverage.missingAuthoritativeScenario, false);
  assert.equal(fallback.context.seed, 11); // Already assembled artifacts never change.
  assert.equal(fallback.context.scenarioProvenance, 'submitted');
});

test('remote runs preserve their snapshot manifest and make unavailable scenario and metrics explicit', () => {
  const collection = new ResultsCollection();
  collection.snapshot(telemetryFrame('7', 'run-1', 1, '4'), 100);
  collection.disconnected(110);
  collection.reconnected(120);
  collection.snapshot(telemetryFrame('8', 'run-1', 2, '6'), 130, true);
  const result = assemble(collection);

  assert.equal(result.context.scenario, null);
  assert.equal(result.context.scenarioProvenance, 'missing');
  assert.equal(result.context.seed, null);
  assert.equal(result.context.publicationInterval.configuredNanoseconds, null);
  assert.equal(result.context.experimentProvenance, 'snapshot');
  assert.deepEqual(result.context.experiment, experimentFixture());
  assert.equal(result.summary.payload, null);
  assert.equal(result.summary.transfers, null);
  assert.equal(result.summary.performance, null);
  assert.equal(result.summary.payloadRate, null);
  assert.equal(result.summary.resources.nodes[0].cpu.utilizationRatio, null);
  assert.equal(result.coverage.joinedAfterStart, true);
  assert.deepEqual(result.coverage.publicationsBeforeFirstReceipt, { first: '1', last: '3' });
  assert.deepEqual(result.coverage.unrecordedPublications, [{ first: '1', last: '3' }, { first: '5', last: '5' }]);
  assert.equal(result.coverage.unrecordedPublicationCount, '4');
  assert.equal(result.coverage.disconnections, 1);
  assert.deepEqual(result.coverage.missingDefinitionRevisions, ['7', '8']);
  assert.equal(result.coverage.missingScenario, true);
  assert.equal(result.coverage.missingExperiment, false);
  assert.equal(result.coverage.complete, false);
  assert.deepEqual(result.collection.markers.map((marker) => marker.kind), ['lateJoin', 'disconnected', 'reconnected', 'gap']);
});

test('a missing terminal sample uses an explicit last observation and reports memory loss', () => {
  const collection = new ResultsCollection({ byteBudget: 14_000, terminalReserveBytes: 0 });
  collection.metadata(current(), 10);
  collection.definition(topologyDefinition(), 20);
  assert.equal(collection.snapshot(measuredFrame(1), 100), true);
  const terminal = measuredFrame(2);
  terminal.topology.nodes[0].tcpPhase = 'x'.repeat(30_000);
  assert.equal(collection.snapshot(terminal, 200, true), false);
  const result = assemble(collection);
  assert.equal(result.summary.basis, 'lastObservation');
  assert.equal(result.summary.payload!.receivedBytes, 100);
  assert.equal(result.coverage.terminalSampleMissing, true);
  assert.equal(result.coverage.memoryLimited, true);
  assert.equal(result.coverage.limits.terminalSnapshotDropped, true);
  assert.equal(result.coverage.limits.droppedSamples, 1);
  assert.deepEqual(result.coverage.unrecordedPublications, [{ first: '2', last: '2' }]);
  assert.equal(result.coverage.complete, false);
});

test('metadata-only records can be assembled without inventing observations or a manifest', () => {
  const collection = new ResultsCollection();
  collection.submitted('run-1', scenarioFixture(), 10);
  const result = assemble(collection, 'run-1', 'stopped');
  assert.equal(result.summary.basis, 'unavailable');
  assert.equal(result.summary.observation, null);
  assert.equal(result.summary.payload, null);
  assert.deepEqual(result.summary.resources.nodes, []);
  assert.equal(result.context.experiment, null);
  assert.equal(result.context.experimentProvenance, 'missing');
  assert.equal(result.coverage.missingExperiment, true);
  assert.equal(result.coverage.terminalSampleMissing, true);
  assert.equal(result.completion.reason, 'stopped');
});

test('metadata replacements charge net serialized bytes and duplicates retain original timing', () => {
  const collection = new ResultsCollection({ byteBudget: 20_000, terminalReserveBytes: 0 });
  const large = scenarioFixture();
  large.fixed.loadBalancer.id = 'x'.repeat(2_000);
  collection.submitted('run-1', large, 10);
  const before = collection.usage.serializedBytes;
  const metadataBefore = collection.get('run-1')!.metadata;
  const source = current();
  assert.equal(collection.metadata(source, 20), true);
  const metadataAfter = collection.get('run-1')!.metadata;
  const bytes = (value: unknown) => new TextEncoder().encode(JSON.stringify(value)).byteLength;
  assert.equal(collection.usage.serializedBytes, before + bytes(metadataAfter) - bytes(metadataBefore));
  assert.ok(collection.usage.serializedBytes < before);
  const retained = collection.usage.serializedBytes;
  assert.equal(collection.metadata(source, 30), false);
  assert.equal(collection.usage.serializedBytes, retained);
  assert.equal(collection.get('run-1')!.metadata!.receivedAt, 20);
  source.scenario.fixed.execution.seed = 999;
  assert.equal(collection.get('run-1')!.metadata!.scenario.fixed.execution.seed, '-9007199254740993');
  collection.release('run-1');
  assert.equal(collection.usage.serializedBytes, 512);
});

test('oversized metadata is rejected visibly and shares the eight-run and byte limits', () => {
  const collection = new ResultsCollection({ byteBudget: 8_000, terminalReserveBytes: 0 });
  const submitted = scenarioFixture();
  collection.submitted('run-1', submitted, 10);
  const oversized = current();
  oversized.scenario.fixed.loadBalancer.id = 'é'.repeat(10_000);
  assert.equal(collection.metadata(oversized, 20), false);
  assert.equal(collection.get('run-1')!.metadata!.provenance, 'submitted');
  assert.equal(collection.get('run-1')!.limits.droppedMetadata, 1);
  assert.equal(collection.get('run-1')!.limits.memoryLimited, true);
  assert.ok(collection.usage.serializedBytes <= 8_000);
  assert.ok(new TextEncoder().encode(JSON.stringify(collection.get('run-1'))).byteLength < collection.usage.serializedBytes);

  const capped = new ResultsCollection();
  for (let index = 0; index < 8; index++) assert.equal(capped.metadata(current(`run-${index}`), 10), true);
  const retainedBytes = capped.usage.serializedBytes;
  assert.equal(capped.submitted('run-nine', submitted, 20), false);
  assert.equal(capped.usage.runCount, 8);
  assert.equal(capped.usage.runLimitReached, true);
  assert.equal(capped.usage.serializedBytes, retainedBytes);
  capped.release('run-0');
  assert.equal(capped.submitted('run-nine', submitted, 30), true);
});

test('invalid completion inputs fail before creating an artifact', () => {
  const collection = new ResultsCollection();
  collection.submitted('run-1', scenarioFixture(), 10);
  const run = collection.get('run-1')!;
  assert.throws(() => assembleRunResult(run, { id: ' ', completion: { reason: 'finished', receivedAt: 20 } }), /ID/);
  assert.throws(() => assembleRunResult(run, { id: 'local', completion: { reason: 'stopped', receivedAt: NaN } }), /timestamp/);
  assert.throws(() => collection.metadata(current(), Infinity), /timestamp/);
  assert.throws(() => collection.submitted('run-1', scenarioFixture(), NaN), /timestamp/);
});
