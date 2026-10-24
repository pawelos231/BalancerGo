import assert from 'node:assert/strict';
import test from 'node:test';
import { hydrateSnapshot } from '../src/telemetry/hydrate.ts';
import { parseSnapshot } from '../src/telemetry/parse.ts';
import { groupTopology } from '../src/topology/grouping.ts';
import { telemetryFrame, topologyDefinition, idleFrame } from './telemetryFixture.ts';

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
