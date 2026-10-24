import type { TopologyLinkStats } from '../src/topology/types.ts';
import type { SimulationSnapshot, TopologyDefinition } from '../src/telemetry/types.ts';

export function linkStats(value = 0): TopologyLinkStats {
  return {
    queuedPackets: 0,
    transmitting: false,
    travellingPackets: 0,
    offeredPackets: value,
    offeredBytes: value * 100,
    acceptedPackets: value,
    acceptedBytes: value * 100,
    queueDroppedPackets: 0,
    queueDroppedBytes: 0,
    lostPackets: 0,
    lostBytes: 0,
    deliveredPackets: value,
    deliveredBytes: value * 100,
  };
}

export function topologyDefinition(revision = '1', runId = 'run-1'): TopologyDefinition {
  const config = {
    bandwidthBitsPerSecond: 1_000_000_000,
    packetTravelTimeMs: 1,
    queueCapacity: 4,
    lossRate: 0,
  };
  const clients = ['a', 'b'].map((id, index) => ({
    id: `client-${id}`,
    kind: 'client',
    label: `Client ${id}`,
    groupId: 'clients',
    address: `192.0.2.${index + 1}:1234`,
    clientKey: {
      SrcIP: `192.0.2.${index + 1}`, SrcPort: 1234,
      DstIP: '198.51.100.1', DstPort: 80, Protocol: 'TCP',
    },
    metrics: [
      { key: 'payloadBytes', label: 'Payload', unit: 'bytes', value: 1024 },
      { key: 'completedStreams', label: 'Completed streams', unit: 'streams' },
    ],
  }));
  return {
    runId,
    revision,
    nodes: [
      { id: 'lb', kind: 'loadBalancer', label: 'LB', metrics: [] },
      { id: 'backend', kind: 'backend', label: 'Backend', metrics: [] },
      ...clients,
    ],
    links: ['a', 'b'].flatMap((id) => [
      { id: `${id}-in`, label: 'Access', source: `client-${id}`, target: 'lb', config: { ...config } },
      { id: `${id}-out`, label: 'Return', source: 'lb', target: `client-${id}`, config: { ...config } },
    ]),
    ingress: {
      config: { ...config },
      queues: [
        { id: 'front', side: 'clients', label: 'Frontside', capacity: 4 },
        { id: 'back', side: 'backends', label: 'Backside', capacity: 4 },
      ],
    },
    servers: [{ id: 'srv', name: 'Original backend', address: '10.0.0.1', port: 80 }],
  };
}

export function telemetryFrame(revision = '1', runId = 'run-1', value = 1): SimulationSnapshot {
  const definition = topologyDefinition(revision, runId);
  return {
    type: 'simulationSnapshot',
    version: 1,
    simulation: { runId, status: 'running', algorithm: 'roundRobin', virtualTimeSeconds: value / 10 },
    mode: 'NAT',
    algorithm: 'RoundRobin',
    servers: [{
      id: 'srv', active: true, activeConnections: 2, halfOpenConnections: 0,
      latencyP95Milli: value, numberOfHandledPackets: value,
      clients: [{ id: 'client-a', retrySyn: value, retryData: 0 }],
    }],
    topology: {
      revision,
      nodes: definition.nodes.map((node) => ({
        id: node.id,
        active: true,
        metrics: { completedStreams: value },
        traffic: { transmitBytesPerSecond: value * 1000, receiveBytesPerSecond: value * 100, sampleWindowSeconds: 0.1 },
      })),
      links: definition.links.map((link) => ({ id: link.id, stats: linkStats(value) })),
      ingress: {
        stats: linkStats(value),
        queues: [{ id: 'front', stats: linkStats(value) }, { id: 'back', stats: linkStats() }],
      },
    },
  };
}

export function idleFrame(): SimulationSnapshot {
  return {
    type: 'simulationSnapshot', version: 1, mode: 'NAT', algorithm: 'RoundRobin',
    simulation: { status: 'idle', algorithm: 'roundRobin', virtualTimeSeconds: 0 },
    servers: [], topology: { revision: '0', nodes: [], links: [] },
  };
}
