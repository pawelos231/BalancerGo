import type { TopologyGraph, TopologyLink, TopologyLinkStats, TopologyNode } from './types';
import type { HostState } from '../types';

// Host cards reuse the graph counters rather than adding a second snapshot payload.
export function nodeHostState(node?: TopologyNode): HostState | undefined {
  if (!node?.metrics.some(metric => metric.key === 'hostCPUEnabled')) return undefined;
  const value = (key: string) => node.metrics.find(metric => metric.key === key)?.value ?? 0;
  return {
    enabled: value('hostCPUEnabled') !== 0,
    cores: value('hostCores'),
    busyCores: value('hostBusyCores'),
    queueCapacity: value('hostQueueCapacity'),
    queuedPackets: value('hostQueuedPackets'),
    maxQueuedPackets: value('hostMaxQueuedPackets'),
    processedPackets: value('hostProcessedPackets'),
    queueDroppedPackets: value('hostQueueDroppedPackets'),
    utilization: value('hostUtilization'),
    busyTimeSeconds: node.metrics.find(metric => metric.key === 'hostBusyTimeSeconds')?.value,
    availableCoreTimeSeconds: node.metrics.find(
      metric => metric.key === 'hostAvailableCoreTimeSeconds',
    )?.value,
  };
}

// Retiring work and earlier CPU usage stay visible when new packets bypass the CPU.
export function showHostCPUWork(host: HostState): boolean {
  return host.enabled || host.busyCores > 0 || host.utilization > 0 ||
    host.queuedPackets > 0 || host.maxQueuedPackets > 0 || host.queueDroppedPackets > 0;
}

// LinkRole separates the resource that owns each waiting queue.
export type LinkRole = 'access' | 'ingress' | 'egress' | 'other';

// Explain the queue shown for one kind of network path.
interface LinkDescription {
  label: string;
  owner: string;
  explanation: string;
}

// Queue descriptions apply to individual links and grouped views.
export const roleDetails: Record<LinkRole, LinkDescription> = {
  access: {
    label: 'Incoming access link',
    owner: 'Access-link FIFO',
    explanation: 'Each incoming path keeps its own FIFO before packets reach the next node.',
  },
  ingress: {
    label: 'Shared LB ingress',
    owner: 'Shared input FIFO',
    explanation:
      'Client packets and backend replies share this FIFO before transmission ' +
      'into the load balancer.',
  },
  egress: {
    label: 'LB output branch',
    owner: 'LB output FIFO',
    explanation:
      'Each LB output branch has its own FIFO. A waiting packet needs both a free ' +
      'branch link and a grant from the shared forwarding budget.',
  },
  other: {
    label: 'Directed link',
    owner: 'Per-link queue as reported',
    explanation: 'The model supplies this directed path’s own configuration and packet counters.',
  },
};

// Direct access ends at an input FIFO; completing that hop has not run the LB handler.
export function describeLink(
  link: TopologyLink,
  nodeById: ReadonlyMap<string, TopologyNode>,
): LinkDescription {
  const role = linkRole(link, nodeById);
  const source = nodeById.get(link.source);
  const target = nodeById.get(link.target);
  if (role === 'access' && target?.kind === 'loadBalancer') {
    const side = source?.kind === 'backend' ? 'Backside' : 'Frontside';
    return {
      label: `${side} access link`,
      owner: 'Sending access-link FIFO',
      explanation:
        'These counters describe the endpoint’s access link. Delivery means arrival ' +
        `at the ${side.toLowerCase()} input FIFO; the shared RX resource serves it before LB processing.`,
    };
  }
  return roleDetails[role];
}

// LinkScopes keeps access, shared input and outgoing counters in separate sets.
export interface LinkScopes {
  access: TopologyLink[];
  ingress: TopologyLink[];
  egress: TopologyLink[];
  other: TopologyLink[];
}

// Add counters across separate links and queues, while keeping their rates separate.
// transmittingLinks counts busy senders.
export interface LinkTotals extends Omit<TopologyLinkStats, 'transmitting'> {
  linkCount: number;
  queueCapacity: number;
  transmittingLinks: number;
}

// Roles follow the supplied endpoints, so graph IDs remain free to change.
export function linkRole(
  link: TopologyLink,
  nodeById: ReadonlyMap<string, TopologyNode>,
): LinkRole {
  const source = nodeById.get(link.source);
  const target = nodeById.get(link.target);

  if (source?.kind === 'loadBalancer') {
    return 'egress';
  }
  if (source?.kind === 'networkAggregator' && target?.kind === 'loadBalancer') {
    return 'ingress';
  }

  const endpointSource = source?.kind === 'client' || source?.kind === 'backend';
  const incomingTarget = target?.kind === 'networkAggregator' || target?.kind === 'loadBalancer';
  return endpointSource && incomingTarget ? 'access' : 'other';
}

// Each physical link belongs to one scope and contributes its counters once.
export function scopeLinks(topology: TopologyGraph): LinkScopes {
  const nodeById = new Map(topology.nodes.map((node) => [node.id, node]));
  const scopes: LinkScopes = { access: [], ingress: [], egress: [], other: [] };
  const seen = new Set<string>();

  for (const link of topology.links) {
    if (!seen.has(link.id)) {
      seen.add(link.id);
      scopes[linkRole(link, nodeById)].push(link);
    }
  }
  return scopes;
}

// Totals count distinct physical hops; they never combine configured bitrates.
export function totalLinks(links: readonly TopologyLink[]): LinkTotals {
  const totals: LinkTotals = {
    linkCount: 0,
    queueCapacity: 0,
    transmittingLinks: 0,
    queuedPackets: 0,
    travellingPackets: 0,
    offeredPackets: 0,
    offeredBytes: 0,
    acceptedPackets: 0,
    acceptedBytes: 0,
    queueDroppedPackets: 0,
    queueDroppedBytes: 0,
    ecnMarkedPackets: 0,
    ecnMarkedBytes: 0,
    congestionDroppedPackets: 0,
    congestionDroppedBytes: 0,
    lostPackets: 0,
    lostBytes: 0,
    deliveredPackets: 0,
    deliveredBytes: 0,
  };
  const seen = new Set<string>();

  for (const link of links) {
    if (seen.has(link.id)) {
      continue;
    }
    seen.add(link.id);
    const { stats } = link;
    totals.linkCount++;
    totals.queueCapacity += link.config.queueCapacity;
    totals.transmittingLinks += stats.transmitting ? 1 : 0;
    totals.queuedPackets += stats.queuedPackets;
    totals.travellingPackets += stats.travellingPackets;
    totals.offeredPackets += stats.offeredPackets;
    totals.offeredBytes += stats.offeredBytes;
    totals.acceptedPackets += stats.acceptedPackets;
    totals.acceptedBytes += stats.acceptedBytes;
    totals.queueDroppedPackets += stats.queueDroppedPackets;
    totals.queueDroppedBytes += stats.queueDroppedBytes;
    totals.ecnMarkedPackets! += stats.ecnMarkedPackets ?? 0;
    totals.ecnMarkedBytes! += stats.ecnMarkedBytes ?? 0;
    totals.congestionDroppedPackets! += stats.congestionDroppedPackets ?? 0;
    totals.congestionDroppedBytes! += stats.congestionDroppedBytes ?? 0;
    totals.lostPackets += stats.lostPackets;
    totals.lostBytes += stats.lostBytes;
    totals.deliveredPackets += stats.deliveredPackets;
    totals.deliveredBytes += stats.deliveredBytes;
  }
  return totals;
}

// A range describes individual limits, never a promised aggregate throughput.
export function linkValueRange(
  links: readonly TopologyLink[],
  valueFor: (link: TopologyLink) => number,
  formatValue: (value: number) => string,
): string {
  if (links.length === 0) {
    return 'No links';
  }

  let minimum = Infinity;
  let maximum = -Infinity;
  for (const link of links) {
    const value = valueFor(link);
    minimum = Math.min(minimum, value);
    maximum = Math.max(maximum, value);
  }
  return minimum === maximum
    ? formatValue(minimum)
    : `${formatValue(minimum)} to ${formatValue(maximum)}`;
}
