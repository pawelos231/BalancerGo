import type { DiagramGraph, TopologyLinkConfig, TopologyMetric } from '../topology/types.ts';
import type { PlannedHost, PlannedTopology } from './plannedTopology.ts';
import type { ExactInteger, ResolvedLinks, ScenarioLinkConfig } from './types.ts';

export interface PlannedNodeResource {
  resource: string;
  slotIndex?: ExactInteger;
}

export interface PlannedGraphLinkDetails {
  label: string;
  config: ScenarioLinkConfig;
  memberCount: ExactInteger;
}

export interface PlannedTopologyGraph {
  graph: DiagramGraph;
  nodeResources: Map<string, PlannedNodeResource>;
  linkDetails: Map<string, PlannedGraphLinkDetails>;
}

// IDs describe configured roles and slots, so edits and time changes keep selection stable.
function nodeId(role: string, resourceId: string): string {
  return `planned:node:${role}:${encodeURIComponent(resourceId)}`;
}

function hostMetrics(host: PlannedHost): TopologyMetric[] {
  return [
    { key: 'hostCores', label: 'Configured cores', value: Number(host.cores) },
    {
      key: 'packetProcessingTimeNanoseconds',
      label: 'CPU cost / packet',
      value: Number(host.packetProcessingTimeNanoseconds),
      unit: 'ns',
    },
    { key: 'hostQueueCapacity', label: 'CPU waiting slots', value: Number(host.queueCapacity) },
  ];
}

// Only diagram labels use Number; inspector settings retain their exact source integers.
function diagramLinkConfig(config: ScenarioLinkConfig): TopologyLinkConfig {
  const { packetTravelTimeNanoseconds, ...settings } = config;
  return {
    ...settings,
    bandwidthBitsPerSecond: Number(config.bandwidthBitsPerSecond),
    packetTravelTimeMs: Number(packetTravelTimeNanoseconds) / 1_000_000,
    queueCapacity: Number(config.queueCapacity),
  };
}

// One default bucket and sparse configured slots keep work independent of birth budgets.
export function projectPlannedTopologyGraph(plan: PlannedTopology): PlannedTopologyGraph {
  const graph: DiagramGraph = { nodes: [], links: [] };
  const nodeResources = new Map<string, PlannedNodeResource>();
  const linkDetails = new Map<string, PlannedGraphLinkDetails>();
  const balancer = plan.loadBalancer;
  const balancerId = nodeId('load-balancer', balancer.id);

  graph.nodes.push({
    id: balancerId,
    kind: 'loadBalancer',
    label: balancer.id,
    resourceId: balancer.id,
    description: `Configured ${balancer.algorithm} · ${balancer.host.cores} CPU cores`,
    metrics: [
      {
        key: 'ingressBitsPerSecond',
        label: 'Shared ingress limit',
        value: Number(balancer.ingress.bandwidthBitsPerSecond),
        unit: 'bit/s',
      },
      {
        key: 'forwardingBitsPerSecond',
        label: 'Shared forwarding budget',
        value: Number(balancer.forwardingBitsPerSecond),
        unit: 'bit/s',
      },
      ...hostMetrics(balancer.host),
    ],
  });
  nodeResources.set(balancerId, { resource: 'loadBalancer' });

  function addEndpointLinks(
    endpointId: string,
    endpointLabel: string,
    network: ResolvedLinks,
    memberCount: bigint,
  ) {
    // Empty generators and reserved slots do not assert any physical client paths.
    if (memberCount === 0n) return;

    for (const direction of ['toLoadBalancer', 'fromLoadBalancer'] as const) {
      const towardBalancer = direction === 'toLoadBalancer';
      const id = `${endpointId}:link:${direction}`;
      const label = towardBalancer
        ? `${endpointLabel} → ${balancer.id}`
        : `${balancer.id} → ${endpointLabel}`;
      const config = network[direction];
      graph.links.push({
        id,
        label,
        source: towardBalancer ? endpointId : balancerId,
        target: towardBalancer ? balancerId : endpointId,
        config: diagramLinkConfig(config),
        memberCount: Number(memberCount),
      });
      linkDetails.set(id, {
        label,
        config: { ...config },
        memberCount: memberCount.toString(),
      });
    }
  }

  for (const group of plan.clientGroups) {
    const budget = BigInt(group.count);
    const inBudgetOverrides = group.overrides.reduce((count, override) => (
      BigInt(override.index) < budget ? count + 1n : count
    ), 0n);
    const defaultSlots = budget - inBudgetOverrides;
    const groupNodeId = nodeId('client-group', group.id);
    const defaultsId = `${groupNodeId}:defaults`;
    const resource = { resource: `group:${group.id}` };

    graph.nodes.push({
      id: defaultsId,
      kind: 'clientGroup',
      label: `${group.id} · defaults`,
      resourceId: group.id,
      groupId: group.id,
      description: `Generator · ${defaultSlots} default slots in lifetime birth budget ${budget}`,
      metrics: [
        { key: 'clientBudget', label: 'Lifetime birth budget', value: Number(budget), unit: 'slots' },
        { key: 'defaultSlots', label: 'Default slots', value: Number(defaultSlots), unit: 'slots' },
      ],
    });
    nodeResources.set(defaultsId, resource);
    addEndpointLinks(defaultsId, `${group.id} default slots`, group.network, defaultSlots);

    for (const override of group.overrides) {
      const slot = BigInt(override.index).toString();
      const reserved = BigInt(slot) >= budget;
      const overrideId = `${groupNodeId}:slot:${slot}`;
      const slotResource = { ...resource, slotIndex: override.index };
      graph.nodes.push({
        id: overrideId,
        kind: 'clientGroup',
        label: `${group.id} · ${reserved ? 'reserved' : 'configured'} slot ${slot}`,
        resourceId: group.id,
        groupId: group.id,
        description: reserved
          ? 'Reserved configured slot outside current birth budget'
          : 'Configured slot override within lifetime birth budget',
        metrics: [
          { key: 'streamBytes', label: 'Payload budget / client', value: Number(override.stream.bytes), unit: 'bytes' },
          { key: 'streamPackets', label: 'Packet fallback / client', value: Number(override.stream.packets) },
        ],
      });
      nodeResources.set(overrideId, slotResource);
      addEndpointLinks(
        overrideId,
        `${group.id} configured slot ${slot}`,
        override.network,
        reserved ? 0n : 1n,
      );
    }
  }

  for (const backend of plan.backends) {
    const id = nodeId('backend', backend.id);
    const resource = { resource: `backend:${backend.id}` };
    graph.nodes.push({
      id,
      kind: 'backend',
      label: backend.name || backend.id,
      resourceId: backend.id,
      address: `${backend.address}:${backend.port}`,
      description: `Configured ${backend.physicalAvailable ? 'available' : 'unavailable'} · ${backend.administrativeState}`,
      metrics: hostMetrics(backend.host),
    });
    nodeResources.set(id, resource);
    addEndpointLinks(id, backend.name || backend.id, backend.network, 1n);
  }

  return { graph, nodeResources, linkDetails };
}
