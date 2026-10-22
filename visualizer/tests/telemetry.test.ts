import assert from 'node:assert/strict';
import test from 'node:test';

import { formatBandwidth } from '../src/topology/format.ts';
import { describeLink, linkValueRange, scopeLinks, totalLinks } from '../src/topology/telemetry.ts';
import type {
  TopologyGraph,
  TopologyLink,
  TopologyLinkConfig,
  TopologyNode,
} from '../src/topology/types.ts';

function node(id: string, kind: string): TopologyNode {
  return { id, kind, label: id, metrics: [] };
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

test('scopes physical links by endpoint kinds and counts each ID only once', () => {
  // The IDs deliberately resemble different roles than their actual metadata.
  const graph: TopologyGraph = {
    nodes: [
      node('infrastructure/lb', 'client'),
      node('client/not-a-client', 'loadBalancer'),
      node('backend/not-a-backend', 'networkAggregator'),
      node('infrastructure/upstream', 'backend'),
      node('future-switch', 'futureSwitch'),
    ],
    links: [
      link('client-access', 'infrastructure/lb', 'backend/not-a-backend'),
      link('backend-return', 'infrastructure/upstream', 'backend/not-a-backend'),
      link('shared-wire', 'backend/not-a-backend', 'client/not-a-client'),
      link('to-client', 'client/not-a-client', 'infrastructure/lb'),
      link('to-backend', 'client/not-a-client', 'infrastructure/upstream'),
      link('legacy-direct-access', 'infrastructure/lb', 'client/not-a-client'),
      link('future-path', 'future-switch', 'backend/not-a-backend'),
      link('missing-endpoint', 'missing', 'client/not-a-client'),
    ],
  };
  const sharedWire = graph.links[2];
  graph.links.push({ ...sharedWire, stats: { ...sharedWire.stats, offeredPackets: 999 } });

  const scopes = scopeLinks(graph);
  assert.deepEqual(scopes.access.map((edge) => edge.id), [
    'client-access', 'backend-return', 'legacy-direct-access',
  ]);
  assert.deepEqual(scopes.ingress.map((edge) => edge.id), ['shared-wire']);
  assert.deepEqual(scopes.egress.map((edge) => edge.id), ['to-client', 'to-backend']);
  assert.deepEqual(scopes.other.map((edge) => edge.id), ['future-path', 'missing-endpoint']);
  assert.equal(scopes.ingress[0], sharedWire);

  const allScopes = Object.values(scopes).flat();
  assert.equal(allScopes.length, 8);
  assert.equal(new Set(allScopes.map((edge) => edge.id)).size, 8);
});

test('totals deduplicate hops and include all progress, byte and drop counters', () => {
  const access = link('first-hop', 'client', 'upstream', { queueCapacity: 4 });
  access.stats = {
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
  const ingress = link('second-hop', 'upstream', 'lb', { queueCapacity: 8 });
  ingress.stats = {
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
  const duplicate = {
    ...access,
    config: { ...access.config, queueCapacity: 1000 },
    stats: { ...access.stats, deliveredBytes: 999_999 },
  };

  const totals = totalLinks([access, ingress, access, duplicate, ingress]);
  assert.deepEqual(totals, {
    linkCount: 2,
    queueCapacity: 12,
    transmittingLinks: 1,
    queuedPackets: 3,
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
  assert.equal(access.stats.deliveredBytes, 300);
  assert.equal(ingress.stats.deliveredBytes, 200);
});

test('configured link ranges describe individual limits instead of summed bitrate', () => {
  const slow = link('slow', 'a', 'b', {
    bandwidthBitsPerSecond: 2_000_000,
    packetTravelTimeMs: 1,
    queueCapacity: 0,
    lossRate: 0.1,
  });
  const fast = link('fast', 'c', 'd', {
    bandwidthBitsPerSecond: 100_000_000,
    packetTravelTimeMs: 3,
    queueCapacity: 8,
    lossRate: 0.3,
  });
  const bitrate = (edge: TopologyLink) => edge.config.bandwidthBitsPerSecond;

  assert.equal(linkValueRange([fast, slow], bitrate, formatBandwidth), '2 Mbit/s to 100 Mbit/s');
  assert.equal(linkValueRange([slow, slow], bitrate, formatBandwidth), '2 Mbit/s');
  assert.equal(linkValueRange([], bitrate, formatBandwidth), 'No links');
  assert.equal(
    linkValueRange([slow, fast], (edge) => edge.config.packetTravelTimeMs, (value) => `${value} ms`),
    '1 ms to 3 ms',
  );
  assert.equal(
    linkValueRange([slow, fast], (edge) => edge.config.queueCapacity, String),
    '0 to 8',
  );
  assert.equal(
    linkValueRange([slow, fast], (edge) => edge.config.lossRate, (value) => `${value * 100}%`),
    '10% to 30%',
  );
});

test('direct endpoint arrows stay separate from shared RX counters and input FIFOs', () => {
  const clientAccess = link('front-in', 'first', 'middle', { queueCapacity: 2 });
  const backendAccess = link('back-in', 'last', 'middle', { queueCapacity: 4 });
  const clientOutput = link('front-out', 'middle', 'first');
  const backendOutput = link('back-out', 'middle', 'last');

  // Access delivery is arrival at an RX queue, before the shared receiver runs.
  clientAccess.stats = {
    ...clientAccess.stats,
    offeredPackets: 8,
    offeredBytes: 800,
    acceptedPackets: 8,
    acceptedBytes: 800,
    deliveredPackets: 8,
    deliveredBytes: 800,
  };
  backendAccess.stats = {
    ...backendAccess.stats,
    offeredPackets: 5,
    offeredBytes: 500,
    acceptedPackets: 5,
    acceptedBytes: 500,
    deliveredPackets: 5,
    deliveredBytes: 500,
  };
  clientOutput.stats.deliveredPackets = 1;
  clientOutput.stats.deliveredBytes = 100;
  backendOutput.stats.deliveredPackets = 2;
  backendOutput.stats.deliveredBytes = 200;

  const graph: TopologyGraph = {
    nodes: [node('first', 'client'), node('middle', 'loadBalancer'), node('last', 'backend')],
    links: [clientAccess, backendAccess, clientOutput, backendOutput],
    ingress: {
      config: {
        bandwidthBitsPerSecond: 1_000_000_000,
        packetTravelTimeMs: 0,
        queueCapacity: 3,
        lossRate: 0,
      },
      stats: {
        queuedPackets: 2,
        transmitting: true,
        travellingPackets: 2,
        offeredPackets: 13,
        offeredBytes: 1300,
        acceptedPackets: 10,
        acceptedBytes: 1000,
        queueDroppedPackets: 3,
        queueDroppedBytes: 300,
        lostPackets: 1,
        lostBytes: 100,
        deliveredPackets: 4,
        deliveredBytes: 400,
      },
      queues: [
        {
          id: 'front-queue',
          side: 'clients',
          label: 'Frontside',
          capacity: 3,
          stats: {
            queuedPackets: 1,
            transmitting: true,
            travellingPackets: 1,
            offeredPackets: 8,
            offeredBytes: 800,
            acceptedPackets: 6,
            acceptedBytes: 600,
            queueDroppedPackets: 2,
            queueDroppedBytes: 200,
            lostPackets: 1,
            lostBytes: 100,
            deliveredPackets: 2,
            deliveredBytes: 200,
          },
        },
        {
          id: 'back-queue',
          side: 'backends',
          label: 'Backside',
          capacity: 3,
          stats: {
            queuedPackets: 1,
            transmitting: false,
            travellingPackets: 1,
            offeredPackets: 5,
            offeredBytes: 500,
            acceptedPackets: 4,
            acceptedBytes: 400,
            queueDroppedPackets: 1,
            queueDroppedBytes: 100,
            lostPackets: 0,
            lostBytes: 0,
            deliveredPackets: 2,
            deliveredBytes: 200,
          },
        },
      ],
    },
  };
  const before = structuredClone(graph);
  const scopes = scopeLinks(graph);
  assert.deepEqual(scopes.access, [clientAccess, backendAccess]);
  assert.deepEqual(scopes.egress, [clientOutput, backendOutput]);
  assert.deepEqual(scopes.ingress, []);
  assert.deepEqual(scopes.other, []);
  assert.equal(Object.values(scopes).flat().length, 4);

  const access = totalLinks(scopes.access);
  const outputs = totalLinks(scopes.egress);
  assert.equal(access.deliveredPackets, 13);
  assert.equal(access.deliveredBytes, 1300);
  assert.equal(access.queueDroppedPackets, 0);
  assert.equal(access.queueCapacity, 6);
  assert.equal(outputs.deliveredPackets, 3);
  assert.equal(outputs.deliveredBytes, 300);
  assert.equal(outputs.queueDroppedPackets, 0);
  assert.equal(totalLinks(scopes.ingress).linkCount, 0);
  assert.equal(
    linkValueRange(scopes.access, (edge) => edge.config.bandwidthBitsPerSecond, formatBandwidth),
    '100 Mbit/s',
  );

  // Metadata carries two sets of waiting slots but only one common bitrate.
  assert.equal(graph.ingress!.queues.reduce((sum, queue) => sum + queue.capacity, 0), 6);
  assert.equal(graph.ingress!.config.queueCapacity, 3);
  assert.equal(graph.ingress!.config.bandwidthBitsPerSecond, 1_000_000_000);
  assert.equal(graph.ingress!.stats.deliveredPackets, 4);
  assert.equal(graph.ingress!.stats.queueDroppedPackets, 3);
  assert.deepEqual(graph, before);

  const nodes = new Map(graph.nodes.map((entry) => [entry.id, entry]));
  assert.equal(describeLink(clientAccess, nodes).label, 'Frontside access link');
  assert.equal(describeLink(backendAccess, nodes).label, 'Backside access link');
  assert.match(describeLink(clientAccess, nodes).explanation, /input FIFO/);
  assert.match(describeLink(backendAccess, nodes).explanation, /before LB processing/);
});
