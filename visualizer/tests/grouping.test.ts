import assert from 'node:assert/strict';
import test from 'node:test';

import { groupTopology } from '../src/topology/grouping.ts';
import type {
  TopologyGraph,
  TopologyLink,
  TopologyLinkConfig,
  TopologyLinkStats,
  TopologyNode,
} from '../src/topology/types.ts';

function emptyStats(): TopologyLinkStats {
  return {
    queuedPackets: 0,
    transmitting: false,
    travellingPackets: 0,
    offeredPackets: 0,
    offeredBytes: 0,
    acceptedPackets: 0,
    acceptedBytes: 0,
    queueDroppedPackets: 0,
    queueDroppedBytes: 0,
    lostPackets: 0,
    lostBytes: 0,
    deliveredPackets: 0,
    deliveredBytes: 0,
  };
}

function client(id: string, groupId = 'pool', active = true): TopologyNode {
  return { id, kind: 'client', label: id, groupId, active, metrics: [] };
}

function link(
  id: string,
  source: string,
  target: string,
  config: Partial<TopologyLinkConfig> = {},
): TopologyLink {
  return {
    id,
    label: id,
    source,
    target,
    config: {
      bandwidthBitsPerSecond: 100_000_000,
      packetTravelTimeMs: 1,
      queueCapacity: 4,
      lossRate: 0,
      ...config,
    },
    stats: emptyStats(),
  };
}

function fixture(): TopologyGraph {
  return {
    nodes: [
      { id: 'up', kind: 'networkAggregator', label: 'Upstream', metrics: [] },
      { id: 'lb', kind: 'loadBalancer', label: 'LB', metrics: [] },
      { id: 'server', kind: 'backend', label: 'Backend', metrics: [] },
      { id: 'opaque-router', kind: 'futureRouter', label: 'Router', groupId: 'pool', metrics: [] },
      client('opaque-a'),
      client('opaque-b', 'pool', false),
    ],
    links: [
      link('shared-ingress', 'up', 'lb', { bandwidthBitsPerSecond: 1_000_000_000 }),
      link('backend-out', 'lb', 'server'),
      link('backend-return', 'server', 'up'),
      link('a-in', 'opaque-a', 'up'),
      link('a-out', 'lb', 'opaque-a'),
      link('b-in', 'opaque-b', 'up'),
      link('b-out', 'lb', 'opaque-b'),
    ],
  };
}

test('collapses only actual clients and preserves shared infrastructure once', () => {
  const input = fixture();
  const result = groupTopology(input, new Set(), null);
  const group = result.groups[0];

  assert.equal(result.groups.length, 1);
  assert.equal(group.id, 'pool');
  assert.equal(group.nodeId, 'view/client-group/pool');
  assert.equal(group.clients.length, 2);
  assert.equal(group.collapsedClients.length, 2);
  assert.equal(group.expanded, false);
  assert.deepEqual(result.graph.nodes.map((node) => node.id), [
    'up', 'lb', 'server', 'opaque-router', group.nodeId,
  ]);
  assert.equal(result.graph.links.filter((edge) => edge.id === 'shared-ingress').length, 1);
  assert.equal(result.graph.links[0], input.links[0]);
  assert.equal(result.graph.links[1], input.links[1]);
  assert.equal(result.graph.links[2], input.links[2]);

  const collapsed = result.graph.nodes.find((node) => node.id === group.nodeId)!;
  assert.equal(collapsed.kind, 'clientGroup');
  assert.equal(collapsed.active, true);
  assert.deepEqual(collapsed.metrics.map(({ key, value }) => [key, value]), [
    ['clientCount', 2], ['activeClients', 1],
  ]);
  assert.equal(result.bundles.size, 2);
  for (const bundle of result.bundles.values()) {
    assert.equal(bundle.links.length, 2);
    assert.equal(bundle.groupId, 'pool');
    const edge = result.graph.links.find((candidate) => candidate.id === bundle.id)!;
    assert.equal(edge.label, '2 × 100 Mbit/s');
    assert.equal(edge.memberCount, 2);
    assert.equal(edge.config.bandwidthBitsPerSecond, 100_000_000);
  }
});

for (const config of [
  { bandwidthBitsPerSecond: 200_000_000 },
  { packetTravelTimeMs: 2 },
  { queueCapacity: 0 },
  { lossRate: 0.1 },
]) {
  test(`keeps heterogeneous ${Object.keys(config)[0]} in separate bundles`, () => {
    const input = fixture();
    Object.assign(input.links.find((edge) => edge.id === 'b-in')!.config, config);
    const result = groupTopology(input, new Set(), null);
    const bundles = [...result.bundles.values()];
    const access = bundles.filter((bundle) => bundle.links.some((edge) => edge.target === 'up'));

    assert.equal(access.length, 2);
    assert.deepEqual(access.map((bundle) => bundle.links.length), [1, 1]);
    assert.equal(bundles.length, 3);
  });
}

test('sums every counter while preserving each physical link capacity', () => {
  const input = fixture();
  const first = input.links.find((edge) => edge.id === 'a-in')!;
  const second = input.links.find((edge) => edge.id === 'b-in')!;
  first.stats = {
    queuedPackets: 1,
    transmitting: true,
    travellingPackets: 2,
    offeredPackets: 10,
    offeredBytes: 1000,
    acceptedPackets: 8,
    acceptedBytes: 800,
    queueDroppedPackets: 2,
    queueDroppedBytes: 200,
    lostPackets: 1,
    lostBytes: 100,
    deliveredPackets: 3,
    deliveredBytes: 300,
  };
  second.stats = {
    queuedPackets: 2,
    transmitting: false,
    travellingPackets: 0,
    offeredPackets: 6,
    offeredBytes: 600,
    acceptedPackets: 5,
    acceptedBytes: 500,
    queueDroppedPackets: 1,
    queueDroppedBytes: 100,
    lostPackets: 1,
    lostBytes: 100,
    deliveredPackets: 2,
    deliveredBytes: 200,
  };
  const result = groupTopology(input, new Set(), null);
  const bundle = [...result.bundles.values()].find((candidate) => candidate.links.includes(first))!;
  const edge = result.graph.links.find((candidate) => candidate.id === bundle.id)!;

  assert.deepEqual(edge.stats, {
    queuedPackets: 3,
    transmitting: true,
    travellingPackets: 2,
    offeredPackets: 16,
    offeredBytes: 1600,
    acceptedPackets: 13,
    acceptedBytes: 1300,
    queueDroppedPackets: 3,
    queueDroppedBytes: 300,
    lostPackets: 2,
    lostBytes: 200,
    deliveredPackets: 5,
    deliveredBytes: 500,
  });
  assert.equal(edge.config.queueCapacity, 4);
  assert.equal(bundle.links.length * edge.config.queueCapacity, 8);
});

for (const selection of [
  { kind: 'node' as const, id: 'opaque-a' },
  { kind: 'link' as const, id: 'a-in' },
  { kind: 'link' as const, id: 'a-out' },
]) {
  test(`physical ${selection.kind} selection reveals only its client and both directions`, () => {
    const input = fixture();
    const result = groupTopology(input, new Set(), selection);
    const group = result.groups[0];

    assert.equal(group.clients.length, 2);
    assert.deepEqual(group.collapsedClients.map((node) => node.id), ['opaque-b']);
    assert.equal(group.expanded, false);
    assert.ok(result.graph.nodes.includes(input.nodes.find((node) => node.id === 'opaque-a')!));
    assert.equal(result.graph.nodes.some((node) => node.id === 'opaque-b'), false);
    for (const id of ['a-in', 'a-out']) {
      assert.ok(result.graph.links.includes(input.links.find((edge) => edge.id === id)!));
    }
    for (const bundle of result.bundles.values()) {
      assert.equal(bundle.links.length, 1);
      assert.equal(bundle.links[0].source === 'opaque-b' || bundle.links[0].target === 'opaque-b', true);
    }
    const collapsed = result.graph.nodes.find((node) => node.id === group.nodeId)!;
    assert.equal(collapsed.metrics[0].value, 1);
    assert.equal(collapsed.metrics[1].value, 0);
  });
}

test('selecting a synthetic node or bundle keeps the whole group collapsed', () => {
  const input = fixture();
  const baseline = groupTopology(input, new Set(), null);
  const selections = [
    { kind: 'node' as const, id: baseline.groups[0].nodeId },
    { kind: 'link' as const, id: [...baseline.bundles.keys()][0] },
  ];

  for (const selection of selections) {
    const result = groupTopology(input, new Set(), selection);
    assert.equal(result.groups[0].collapsedClients.length, 2);
    assert.deepEqual(result.graph, baseline.graph);
  }
});

test('manual expansion preserves the actual graph and retains group controls', () => {
  const input = fixture();
  const result = groupTopology(input, new Set(['pool']), null);

  assert.equal(result.graph, input);
  assert.equal(result.bundles.size, 0);
  assert.equal(result.groups[0].expanded, true);
  assert.equal(result.groups[0].clients.length, 2);
  assert.deepEqual(result.groups[0].collapsedClients, []);
});

test('a selected physical edge exposes both clients and removes an empty group node', () => {
  const input = fixture();
  input.links.push(link('peer', 'opaque-a', 'opaque-b'));
  const result = groupTopology(input, new Set(), { kind: 'link', id: 'peer' });

  assert.equal(result.graph, input);
  assert.equal(result.groups.length, 1);
  assert.equal(result.groups[0].expanded, false);
  assert.deepEqual(result.groups[0].collapsedClients, []);
});

test('single clients, missing group metadata and unknown kinds remain physical nodes', () => {
  const input: TopologyGraph = {
    nodes: [
      client('one', 'single'),
      { id: 'client/not-metadata/0', kind: 'client', label: 'No group', metrics: [] },
      { id: 'unknown-a', kind: 'futureHost', label: 'A', groupId: 'future', metrics: [] },
      { id: 'unknown-b', kind: 'futureHost', label: 'B', groupId: 'future', metrics: [] },
    ],
    links: [],
  };
  const result = groupTopology(input, new Set(), null);

  assert.equal(result.graph, input);
  assert.deepEqual(result.groups, []);
});

test('statistics updates preserve projection IDs and order without mutating the DTO', () => {
  const input = fixture();
  const originalJSON = JSON.stringify(input);
  const baseline = groupTopology(input, new Set(), null);
  assert.equal(JSON.stringify(input), originalJSON);

  const update = structuredClone(input);
  for (const edge of update.links) {
    edge.stats.offeredPackets += 7;
    edge.stats.queuedPackets += 1;
    edge.stats.transmitting = true;
  }
  update.nodes.find((node) => node.id === 'opaque-b')!.active = true;
  const next = groupTopology(update, new Set(), null);

  assert.deepEqual(next.graph.nodes.map((node) => node.id), baseline.graph.nodes.map((node) => node.id));
  assert.deepEqual(
    next.graph.links.map(({ id, source, target }) => ({ id, source, target })),
    baseline.graph.links.map(({ id, source, target }) => ({ id, source, target })),
  );
  assert.deepEqual([...next.bundles.keys()], [...baseline.bundles.keys()]);
  const groupNode = next.graph.nodes.find((node) => node.id === next.groups[0].nodeId)!;
  assert.equal(groupNode.metrics[1].value, 2);
  assert.equal(JSON.stringify(input), originalJSON);
});

test('synthetic IDs cannot replace actual resources using the same view prefix', () => {
  const input = fixture();
  const baseline = groupTopology(input, new Set(), null);
  const occupiedNodeId = baseline.groups[0].nodeId;
  const occupiedLinkId = [...baseline.bundles.keys()][0];
  const unknown = { id: occupiedNodeId, kind: 'futureHost', label: 'Actual resource', metrics: [] };
  const actualLink = link(occupiedLinkId, occupiedNodeId, 'lb');
  input.nodes.push(unknown);
  input.links.push(actualLink);

  const result = groupTopology(input, new Set(), null);
  assert.ok(result.graph.nodes.includes(unknown));
  assert.ok(result.graph.links.includes(actualLink));
  assert.notEqual(result.groups[0].nodeId, occupiedNodeId);
  assert.equal(new Set(result.graph.nodes.map((node) => node.id)).size, result.graph.nodes.length);
  assert.equal(new Set(result.graph.links.map((edge) => edge.id)).size, result.graph.links.length);
});

test('large groups stay collapsed when a physical client is selected', () => {
  const input = fixture();
  input.nodes = input.nodes.filter((node) => node.kind !== 'client');
  input.links = input.links.slice(0, 3);
  for (let index = 0; index < 10_000; index++) {
    const id = `opaque-${index}`;
    input.nodes.push(client(id));
    input.links.push(link(`${id}-in`, id, 'up'), link(`${id}-out`, 'lb', id));
  }

  const result = groupTopology(input, new Set(), { kind: 'node', id: 'opaque-9999' });
  assert.equal(result.groups[0].clients.length, 10_000);
  assert.equal(result.groups[0].collapsedClients.length, 9_999);
  assert.equal(result.graph.nodes.length, 6);
  assert.equal(result.graph.links.length, 7);
  assert.equal(result.bundles.size, 2);
  assert.ok(result.graph.links.some((edge) => edge.id === 'opaque-9999-in'));
  assert.ok(result.graph.links.some((edge) => edge.id === 'opaque-9999-out'));
});

test('empty snapshots have no groups, bundles or invented resources', () => {
  const input: TopologyGraph = { nodes: [], links: [] };
  const result = groupTopology(input, new Set(['old-group']), null);

  assert.equal(result.graph, input);
  assert.deepEqual(result.groups, []);
  assert.equal(result.bundles.size, 0);
});

test('grouping preserves one shared receiver and two input queues without extra hops', () => {
  const input = fixture();
  input.nodes = input.nodes.filter((node) => node.kind !== 'networkAggregator');
  input.links = input.links
    .filter((edge) => edge.id !== 'shared-ingress')
    .map((edge) => ({ ...edge, target: edge.target === 'up' ? 'lb' : edge.target }));
  input.ingress = {
    config: { ...input.links[0].config, queueCapacity: 4 },
    stats: { ...emptyStats(), queuedPackets: 6 },
    queues: [
      {
        id: 'rx-clients', side: 'clients', label: 'From clients', capacity: 4,
        stats: { ...emptyStats(), queuedPackets: 4 },
      },
      {
        id: 'rx-backends', side: 'backends', label: 'From backends', capacity: 4,
        stats: { ...emptyStats(), queuedPackets: 2 },
      },
    ],
  };

  const grouped = groupTopology(input, new Set(), null);
  assert.equal(grouped.graph.ingress, input.ingress);
  assert.equal(grouped.graph.ingress.queues.length, 2);
  assert.equal(grouped.graph.ingress.stats.queuedPackets, 6);
  assert.equal(grouped.graph.ingress.config.queueCapacity, 4);
  assert.ok(grouped.graph.nodes.every((node) => node.kind !== 'networkAggregator'));
  assert.ok(grouped.graph.links.every((edge) => edge.source !== 'lb' || edge.target !== 'lb'));

  const expanded = groupTopology(input, new Set(['pool']), null);
  assert.equal(expanded.graph.ingress, input.ingress);
});
