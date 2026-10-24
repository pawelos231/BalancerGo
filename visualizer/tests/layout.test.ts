import assert from 'node:assert/strict';
import test from 'node:test';

import {
  layoutTopology,
  MAX_VISIBLE_LINKS,
  MAX_VISIBLE_NODES,
  NODE_HEIGHT,
  nodeHeight,
  structureKey,
  visibleGraph,
} from '../src/topology/layout.ts';
import type { TopologyGraph, TopologyLink, TopologyNode } from '../src/topology/types.ts';

function node(id: string, kind: string): TopologyNode {
  return { id, kind, label: id, metrics: [] };
}

function link(id: string, source: string, target: string): TopologyLink {
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
    },
    stats: {
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
    },
  };
}

function expandedGraph(count: number): TopologyGraph {
  const nodes: TopologyNode[] = [];
  const links: TopologyLink[] = [];

  for (let index = 0; index < count; index++) {
    const id = `opaque-${index}`;
    nodes.push(node(id, 'client'));
    links.push(
      link(`access-${index}`, id, 'shared-receiver'),
      link(`return-${index}`, 'shared-sender', id),
    );
  }

  // Shared resources are deliberately last, beyond the ordinary node slice.
  nodes.push(
    node('shared-sender', 'loadBalancer'),
    node('shared-receiver', 'networkAggregator'),
  );
  links.push(link('common-ingress', 'shared-receiver', 'shared-sender'));
  return { nodes, links };
}

function endpointGraph(clientCount = 2): TopologyGraph {
  const nodes: TopologyNode[] = [];
  const links: TopologyLink[] = [];

  for (let index = 0; index < clientCount; index++) {
    const id = `endpoint-${index}`;
    nodes.push(node(id, index === 0 ? 'clientGroup' : 'client'));
    links.push(
      link(`front-in-${index}`, id, 'middle'),
      link(`front-out-${index}`, 'middle', id),
    );
  }
  nodes.push(node('middle', 'loadBalancer'), node('right-side', 'backend'));
  links.push(
    link('back-in', 'right-side', 'middle'),
    link('back-out', 'middle', 'right-side'),
  );

  const emptyStats = link('template', '', '').stats;
  return {
    nodes,
    links,
    ingress: {
      config: {
        bandwidthBitsPerSecond: 1_000_000_000,
        packetTravelTimeMs: 0,
        queueCapacity: 3,
        lossRate: 0,
      },
      stats: { ...emptyStats, queuedPackets: 2, transmitting: true },
      queues: [
        {
          id: 'front-queue',
          side: 'clients',
          label: 'Frontside',
          capacity: 3,
          stats: { ...emptyStats, queuedPackets: 1, transmitting: true },
        },
        {
          id: 'back-queue',
          side: 'backends',
          label: 'Backside',
          capacity: 3,
          stats: { ...emptyStats, queuedPackets: 1 },
        },
      ],
    },
  };
}

function assertValidBoundedGraph(graph: TopologyGraph) {
  assert.ok(graph.nodes.length <= MAX_VISIBLE_NODES);
  assert.ok(graph.links.length <= MAX_VISIBLE_LINKS);
  const nodeIds = new Set(graph.nodes.map((entry) => entry.id));
  assert.equal(nodeIds.size, graph.nodes.length);
  assert.equal(new Set(graph.links.map((entry) => entry.id)).size, graph.links.length);

  for (const edge of graph.links) {
    assert.ok(nodeIds.has(edge.source), `missing source for ${edge.id}`);
    assert.ok(nodeIds.has(edge.target), `missing target for ${edge.id}`);
  }
}

test('a deep client selection survives the node cap with shared resources and both paths', () => {
  const input = expandedGraph(10_000);
  const graph = visibleGraph(input, { kind: 'node', id: 'opaque-9999' });
  const ids = new Set(graph.nodes.map((entry) => entry.id));

  assertValidBoundedGraph(graph);
  assert.equal(graph.nodes.length, MAX_VISIBLE_NODES);
  assert.ok(ids.has('opaque-9999'));
  assert.ok(ids.has('shared-sender'));
  assert.ok(ids.has('shared-receiver'));
  assert.ok(graph.links.some((edge) => edge.id === 'access-9999'));
  assert.ok(graph.links.some((edge) => edge.id === 'return-9999'));
  assert.ok(graph.links.some((edge) => edge.id === 'common-ingress'));
  assert.equal(input.nodes.length, 10_002);
  assert.equal(input.links.length, 20_001);
});

test('a selected physical edge remains reachable after both canvas caps are reached', () => {
  const input = expandedGraph(1000);
  const parallel = Array.from({ length: MAX_VISIBLE_LINKS + 20 }, (_, index) => (
    link(`parallel-${index}`, 'shared-sender', 'opaque-0')
  ));
  input.links.unshift(...parallel);

  const graph = visibleGraph(input, { kind: 'link', id: 'return-999' });
  const ids = new Set(graph.nodes.map((entry) => entry.id));

  assertValidBoundedGraph(graph);
  assert.equal(graph.nodes.length, MAX_VISIBLE_NODES);
  assert.equal(graph.links.length, MAX_VISIBLE_LINKS);
  assert.ok(ids.has('opaque-999'));
  assert.ok(ids.has('shared-sender'));
  assert.ok(ids.has('shared-receiver'));
  assert.ok(graph.links.includes(input.links.find((edge) => edge.id === 'return-999')!));
  assert.ok(graph.links.some((edge) => edge.id === 'common-ingress'));
});

test('layout keys ignore live values and settings while retaining actual structure', () => {
  const original = expandedGraph(2);
  const update = structuredClone(original);
  for (const entry of update.nodes) {
    entry.label = `Updated ${entry.label}`;
    entry.active = false;
    entry.metrics = [{ key: 'packets', label: 'Packets', value: 1000 }];
    entry.traffic = {
      transmitBytesPerSecond: 125_000_000,
      receiveBytesPerSecond: 625_000,
      sampleWindowSeconds: 0.1,
    };
  }
  for (const edge of update.links) {
    edge.label = `Updated ${edge.label}`;
    edge.config.bandwidthBitsPerSecond = 1_000_000;
    edge.config.packetTravelTimeMs = 7;
    edge.config.queueCapacity = 0;
    edge.config.lossRate = 0.5;
    edge.memberCount = 8;
    edge.stats.queuedPackets = 2;
    edge.stats.transmitting = true;
    edge.stats.deliveredBytes = 10_000;
  }

  const originalKey = structureKey(original);
  assert.equal(structureKey(update), originalKey);

  // The same IDs wired differently require a new placement.
  update.links[0].target = 'shared-sender';
  assert.notEqual(structureKey(update), originalKey);
  update.links[0].target = original.links[0].target;

  update.links[0].id = 'replacement-edge';
  assert.notEqual(structureKey(update), originalKey);
  update.links[0].id = original.links[0].id;

  update.nodes[0].id = 'replacement-node';
  assert.notEqual(structureKey(update), originalKey);
  update.nodes[0].id = original.nodes[0].id;

  update.nodes[0].kind = 'futureRouter';
  assert.notEqual(structureKey(update), originalKey);
});

test('endpoint stars keep clients and backends on their own LB sides and ports', () => {
  const graph = endpointGraph();
  // Link order and arbitrary IDs must not turn a reply into the forward path.
  graph.links.reverse();
  const layout = layoutTopology(structureKey(graph));
  const balancer = layout.positions.get('middle')!;
  assert.equal(layout.domainLayout, true);
  assert.ok(nodeHeight('loadBalancer') > NODE_HEIGHT);
  assert.equal(nodeHeight('clientGroup'), NODE_HEIGHT);
  assert.equal(nodeHeight('futureRouter'), NODE_HEIGHT);

  for (const entry of graph.nodes) {
    const position = layout.positions.get(entry.id)!;
    assert.ok(Number.isFinite(position.x) && Number.isFinite(position.y));
    if (entry.kind === 'client' || entry.kind === 'clientGroup') {
      assert.ok(position.x < balancer.x, `${entry.id} must stay on the front side`);
    } else if (entry.kind === 'backend') {
      assert.ok(position.x > balancer.x, `${entry.id} must stay on the back side`);
    }
  }

  const upperPorts = { sourceHandle: 'out-right', targetHandle: 'in-left', lane: -1 };
  const lowerPorts = { sourceHandle: 'out-left', targetHandle: 'in-right', lane: 1 };
  assert.deepEqual(layout.routes.get('front-in-0'), upperPorts);
  assert.deepEqual(layout.routes.get('front-in-1'), upperPorts);
  assert.deepEqual(layout.routes.get('front-out-0'), lowerPorts);
  assert.deepEqual(layout.routes.get('front-out-1'), lowerPorts);
  assert.deepEqual(layout.routes.get('back-out'), upperPorts);
  assert.deepEqual(layout.routes.get('back-in'), lowerPorts);

  const firstClient = layout.positions.get('endpoint-0')!;
  const secondClient = layout.positions.get('endpoint-1')!;
  assert.ok(Math.abs(firstClient.y - secondClient.y) >= NODE_HEIGHT);
});

test('RX queue updates preserve endpoint positions and routes without relayout', () => {
  const graph = endpointGraph();
  const update = structuredClone(graph);
  update.ingress!.config.bandwidthBitsPerSecond = 10_000_000;
  update.ingress!.config.queueCapacity = 0;
  update.ingress!.stats.queuedPackets = 0;
  update.ingress!.stats.deliveredBytes = 123_456;
  for (const queue of update.ingress!.queues) {
    queue.capacity = 0;
    queue.stats.queuedPackets = 0;
    queue.stats.queueDroppedPackets = 20;
  }
  for (const entry of update.nodes) {
    entry.metrics = [{ key: 'queue', label: 'Waiting', value: 20 }];
  }

  assert.equal(structureKey(update), structureKey(graph));
  assert.deepEqual(
    layoutTopology(structureKey(update)),
    layoutTopology(structureKey(graph)),
  );
});

test('unknown and legacy aggregator graphs retain generic layout and all routes', () => {
  const future = endpointGraph(1);
  future.nodes.push(node('router', 'futureRouter'));
  future.links.push(link('future-edge', 'router', 'middle'));

  for (const graph of [future, expandedGraph(2)]) {
    const layout = layoutTopology(structureKey(graph));
    assert.equal(layout.domainLayout, false);
    assert.equal(layout.positions.size, graph.nodes.length);
    assert.equal(layout.routes.size, graph.links.length);
    for (const position of layout.positions.values()) {
      assert.ok(Number.isFinite(position.x) && Number.isFinite(position.y));
    }
    for (const route of layout.routes.values()) {
      assert.match(route.sourceHandle, /^out-(left|right)$/);
      assert.match(route.targetHandle, /^in-(left|right)$/);
    }
  }
});

test('canvas clipping preserves one shared RX resource and separate input capacities', () => {
  const input = endpointGraph(1000);
  input.links.unshift(...Array.from({ length: MAX_VISIBLE_LINKS + 20 }, (_, index) => (
    link(`extra-output-${index}`, 'middle', 'endpoint-0')
  )));
  const graph = visibleGraph(input, { kind: 'link', id: 'front-in-999' });

  assertValidBoundedGraph(graph);
  assert.equal(graph.nodes.length, MAX_VISIBLE_NODES);
  assert.equal(graph.links.length, MAX_VISIBLE_LINKS);
  assert.ok(graph.nodes.some((entry) => entry.id === 'middle'));
  assert.ok(graph.nodes.some((entry) => entry.id === 'endpoint-999'));
  assert.ok(graph.links.some((edge) => edge.id === 'front-in-999'));

  // RX metadata is retained once; no synthetic wire or receiver node is added.
  assert.equal(graph.ingress, input.ingress);
  assert.equal(graph.ingress!.config.bandwidthBitsPerSecond, 1_000_000_000);
  assert.equal(graph.ingress!.queues.length, 2);
  assert.equal(graph.ingress!.queues.reduce((sum, queue) => sum + queue.capacity, 0), 6);
  assert.equal(graph.ingress!.config.queueCapacity, 3);
  assert.equal(graph.nodes.some((entry) => entry.kind === 'networkAggregator'), false);
  const actualLinks = new Set(input.links);
  assert.ok(graph.links.every((edge) => actualLinks.has(edge)));
  assert.equal(input.nodes.length, 1002);
});
