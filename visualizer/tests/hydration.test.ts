import assert from 'node:assert/strict';
import test from 'node:test';
import type { SimulationTransferResults } from '../src/types.ts';
import { hydrateSnapshot, idleSnapshot } from '../src/telemetry/hydrate.ts';
import { parseSnapshot } from '../src/telemetry/parse.ts';
import { groupTopology } from '../src/topology/grouping.ts';
import { telemetryFrame, topologyDefinition, idleFrame } from './telemetryFixture.ts';

function transferResults(): SimulationTransferResults {
  return {
    started: 3, notStarted: 2, succeeded: 1, failed: 1, inProgress: 1,
    failures: { dataRetryLimit: 1 }, synRetransmissions: 4, dataRetransmissions: 7,
    handshake: { samples: 3, meanSeconds: 0.02, minSeconds: 0.01, maxSeconds: 0.03 },
    payload: { samples: 1, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 },
    completion: { samples: 1, meanSeconds: 0.03, minSeconds: 0.03, maxSeconds: 0.03 },
  };
}

test('hydration reuses settings, resolves routed keys and preserves measured traffic', () => {
  const definition = topologyDefinition();
  const frame = telemetryFrame();
  const view = hydrateSnapshot(frame, definition);
  assert.equal(view.topology?.links[0].config, definition.links[0].config);
  assert.equal(view.topology?.ingress?.config, definition.ingress?.config);
  assert.equal(view.servers[0].server.Name, 'Original backend');
  assert.equal(view.servers[0].clients[0].Key, definition.nodes[2].clientKey);
  assert.equal(view.servers[0].clients[0].RetrySyn, 1);
  const client = view.topology!.nodes.find((node) => node.id === 'client-a')!;
  assert.equal(client.metrics[0], definition.nodes[2].metrics[0]);
  assert.equal(client.metrics[1].value, 1);
  assert.equal(client.traffic, frame.topology.nodes[2].traffic);

  const grouped = groupTopology(view.topology!, new Set(), { kind: 'node', id: 'client-a' });
  assert.equal(grouped.graph.nodes.find((node) => node.kind === 'clientGroup')?.traffic?.transmitBytesPerSecond, 1000);
});

test('complete independent frames replace false, zero and missing traffic without stale values', () => {
  const definition = topologyDefinition();
  const first = hydrateSnapshot(telemetryFrame('1', 'run-1', 20), definition);
  const next = telemetryFrame('1', 'run-1', 0);
  next.topology.nodes[2].active = false;
  delete next.topology.nodes[2].traffic;
  const view = hydrateSnapshot(next, definition);
  assert.equal(first.topology!.nodes[2].metrics[1].value, 20);
  assert.equal(view.topology!.nodes[2].metrics[1].value, 0);
  assert.equal(view.topology!.nodes[2].active, false);
  assert.equal(view.topology!.nodes[2].traffic, undefined);
  assert.equal(view.topology!.links[0].stats.deliveredBytes, 0);
});

test('hydration refuses revision/run mismatches and missing resources', () => {
  assert.throws(() => hydrateSnapshot(telemetryFrame('2'), topologyDefinition('1')), /different runs or topology revisions/);
  assert.throws(() => hydrateSnapshot(telemetryFrame('1', 'other'), topologyDefinition()), /different runs or topology revisions/);
  const missingKey = topologyDefinition();
  delete missingKey.nodes[2].clientKey;
  assert.throws(() => hydrateSnapshot(telemetryFrame(), missingKey), /do not cover/);
  const missingNode = telemetryFrame();
  missingNode.topology.nodes.pop();
  assert.throws(() => hydrateSnapshot(missingNode, topologyDefinition()), /resource set/);
});

test('only supported complete snapshot envelopes enable the live model', () => {
  assert.equal(parseSnapshot(telemetryFrame()).version, 1);
  assert.equal(parseSnapshot(idleFrame()).simulation.status, 'idle');
  assert.throws(() => parseSnapshot({ ...telemetryFrame(), version: 2 }), /unsupported/);
  assert.throws(() => parseSnapshot({ ...telemetryFrame(), simulation: {} }), /incomplete/);
});

test('payload hydration preserves live totals, zero measurements and topology counters', () => {
  const frame = telemetryFrame();
  const definition = topologyDefinition();
  frame.simulation.payload = { receivedBytes: 4096, acknowledgedBytes: 2048 };
  frame.servers[0].receivedPayloadBytes = 4096;
  frame.servers[0].clients[0].acknowledgedPayloadBytes = 0;
  definition.nodes[0].metrics.push(
    { key: 'receivedPayloadBytes', label: 'Payload received', unit: 'bytes' },
    { key: 'acknowledgedPayloadBytes', label: 'Payload acknowledged', unit: 'bytes' },
  );
  definition.nodes[1].metrics.push(
    { key: 'receivedPayloadBytes', label: 'Payload received', unit: 'bytes' },
  );
  definition.nodes.slice(2).forEach((node) => node.metrics.push(
    { key: 'acknowledgedPayloadBytes', label: 'Payload acknowledged', unit: 'bytes' },
  ));
  frame.topology.nodes[0].metrics.receivedPayloadBytes = 4096;
  frame.topology.nodes[0].metrics.acknowledgedPayloadBytes = 2048;
  frame.topology.nodes[1].metrics.receivedPayloadBytes = 4096;
  frame.topology.nodes[2].metrics.acknowledgedPayloadBytes = 0;
  frame.topology.nodes[3].metrics.acknowledgedPayloadBytes = 2048;

  const view = hydrateSnapshot(parseSnapshot(frame), definition);
  assert.deepEqual(view.simulation.payload, { receivedBytes: 4096, acknowledgedBytes: 2048 });
  assert.equal(view.servers[0].server.ReceivedPayloadBytes, 4096);
  assert.equal(view.servers[0].clients[0].AcknowledgedPayloadBytes, 0);
  assert.equal(view.topology!.nodes[0].metrics.find((metric) => metric.key === 'receivedPayloadBytes')?.value, 4096);
  assert.equal(view.topology!.nodes[1].metrics.find((metric) => metric.key === 'receivedPayloadBytes')?.value, 4096);
  const grouped = groupTopology(view.topology!, new Set(), null);
  const clientGroup = grouped.graph.nodes.find((node) => node.kind === 'clientGroup')!;
  assert.equal(clientGroup.metrics.find((metric) => metric.key === 'acknowledgedPayloadBytes')?.value, 2048);
});

test('payload totals survive routed connection removal and disappear on idle reset', () => {
  for (const status of ['running', 'finished', 'failed'] as const) {
    const frame = telemetryFrame();
    frame.simulation.status = status;
    frame.simulation.payload = { receivedBytes: 8192, acknowledgedBytes: 4096 };
    frame.servers[0].receivedPayloadBytes = 8192;
    frame.servers[0].clients = [];
    const view = hydrateSnapshot(frame, topologyDefinition());
    assert.deepEqual(view.simulation.payload, { receivedBytes: 8192, acknowledgedBytes: 4096 });
    assert.equal(view.servers[0].server.ReceivedPayloadBytes, 8192);
    assert.deepEqual(view.servers[0].clients, []);
  }
  assert.equal(idleSnapshot(idleFrame()).simulation.payload, undefined);
});

test('omitted payload measurements stay unknown while an explicit zero stays known', () => {
  const frame = telemetryFrame();
  const legacy = hydrateSnapshot(frame, topologyDefinition());
  assert.equal(legacy.simulation.payload, undefined);
  assert.equal(legacy.servers[0].server.ReceivedPayloadBytes, undefined);
  assert.equal(legacy.servers[0].clients[0].AcknowledgedPayloadBytes, undefined);

  frame.simulation.payload = { receivedBytes: 0, acknowledgedBytes: 0 };
  frame.servers[0].receivedPayloadBytes = 0;
  frame.servers[0].clients[0].acknowledgedPayloadBytes = 0;
  const zero = hydrateSnapshot(frame, topologyDefinition());
  assert.deepEqual(zero.simulation.payload, { receivedBytes: 0, acknowledgedBytes: 0 });
  assert.equal(zero.servers[0].server.ReceivedPayloadBytes, 0);
  assert.equal(zero.servers[0].clients[0].AcknowledgedPayloadBytes, 0);
});

test('transfer hydration preserves duration samples, failure reasons and cumulative retries', () => {
  const frame = telemetryFrame();
  const definition = topologyDefinition();
  frame.simulation.transfers = transferResults();
  frame.servers[0].clients[0].transfer = {
    status: 'failed', failureReason: 'dataRetryLimit', elapsedSeconds: 4,
    handshakeCompleted: true, handshakeDurationSeconds: 0.02, payloadDurationSeconds: 0,
    synRetransmissions: 2, dataRetransmissions: 5,
  };
  definition.nodes[0].metrics.push({ key: 'failedTransfers', label: 'Failed transfers' });
  definition.nodes[2].metrics.push({ key: 'dataRetransmissions', label: 'Data retransmissions' });
  frame.topology.nodes[0].metrics.failedTransfers = 1;
  frame.topology.nodes[2].metrics.dataRetransmissions = 5;

  const view = hydrateSnapshot(parseSnapshot(frame), definition);
  assert.deepEqual(view.simulation.transfers, frame.simulation.transfers);
  assert.deepEqual(view.servers[0].clients[0].Transfer, frame.servers[0].clients[0].transfer);
  assert.deepEqual(view.simulation.transfers?.payload, {
    samples: 1, meanSeconds: 0, minSeconds: 0, maxSeconds: 0,
  });
  assert.equal(view.topology!.nodes[0].metrics.find((metric) => metric.key === 'failedTransfers')?.value, 1);
  assert.equal(view.topology!.nodes[2].metrics.find((metric) => metric.key === 'dataRetransmissions')?.value, 5);
});

test('omitted transfer measurements stay unknown and fresh zero samples replace previous results', () => {
  const frame = telemetryFrame();
  const definition = topologyDefinition();
  const legacy = hydrateSnapshot(frame, definition);
  assert.equal(legacy.simulation.transfers, undefined);
  assert.equal(legacy.servers[0].clients[0].Transfer, undefined);

  frame.simulation.transfers = transferResults();
  const previous = hydrateSnapshot(frame, definition);
  const next = telemetryFrame();
  next.simulation.transfers = {
    started: 0, notStarted: 5, succeeded: 0, failed: 0, inProgress: 0,
    failures: {}, synRetransmissions: 0, dataRetransmissions: 0,
    handshake: { samples: 0, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 },
    payload: { samples: 0, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 },
    completion: { samples: 0, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 },
  };
  const zero = hydrateSnapshot(next, definition);
  assert.equal(previous.simulation.transfers?.succeeded, 1);
  assert.deepEqual(zero.simulation.transfers, next.simulation.transfers);
  assert.equal(zero.simulation.transfers?.handshake.samples, 0);

  const omitted = hydrateSnapshot(telemetryFrame(), definition);
  assert.equal(omitted.simulation.transfers, undefined);
  assert.equal(omitted.servers[0].clients[0].Transfer, undefined);
});

test('transfer totals survive connection removal and terminal snapshots, then clear on reset', () => {
  for (const status of ['running', 'finished', 'failed'] as const) {
    const frame = telemetryFrame();
    frame.simulation.status = status;
    frame.simulation.transfers = transferResults();
    frame.servers[0].clients = [];
    const view = hydrateSnapshot(frame, topologyDefinition());
    assert.deepEqual(view.simulation.transfers, transferResults());
    assert.deepEqual(view.servers[0].clients, []);
  }
  const reset = idleSnapshot(idleFrame());
  assert.equal(reset.simulation.transfers, undefined);
  assert.deepEqual(reset.servers, []);
});
