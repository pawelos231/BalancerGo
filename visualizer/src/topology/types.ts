// Snapshot DTOs describe the physical graph; grouping adds explicit view metadata.
export interface TopologyMetric {
  key: string;
  label: string;
  value: number;
  unit?: string;
}

export interface TopologyNode {
  id: string;
  kind: string;
  label: string;
  resourceId?: string;
  groupId?: string;
  description?: string;
  address?: string;
  active?: boolean;
  metrics: TopologyMetric[];
}

export interface TopologyLinkConfig {
  bandwidthBitsPerSecond: number;
  packetTravelTimeMs: number;
  queueCapacity: number;
  lossRate: number;
}

export interface TopologyLinkStats {
  queuedPackets: number;
  transmitting: boolean;
  travellingPackets: number;
  offeredPackets: number;
  offeredBytes: number;
  acceptedPackets: number;
  acceptedBytes: number;
  queueDroppedPackets: number;
  queueDroppedBytes: number;
  lostPackets: number;
  lostBytes: number;
  deliveredPackets: number;
  deliveredBytes: number;
}

export interface TopologyLink {
  id: string;
  label: string;
  source: string;
  target: string;
  config: TopologyLinkConfig;
  stats: TopologyLinkStats;
  memberCount?: number; // UI bundles only: number of independent physical links.
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  links: TopologyLink[];
  ingress?: TopologyIngress;
}

// Separate input queues feed one shared physical receiver and bitrate limit.
export interface TopologyIngress {
  config: TopologyLinkConfig;
  stats: TopologyLinkStats;
  queues: TopologyInputQueue[];
}

// Counters belong to one input side; capacity counts its waiting slots only.
export interface TopologyInputQueue {
  id: string;
  side: 'clients' | 'backends';
  label: string;
  capacity: number;
  stats: TopologyLinkStats;
}

export type TopologySelection = {
  kind: 'node' | 'link';
  id: string;
} | null;

// Search entries refer to either a physical resource or a displayed group.
export interface TopologySearchOption {
  kind: 'node' | 'link';
  id: string;
  label: string;
  detail: string;
}
