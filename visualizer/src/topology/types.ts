// Snapshot DTOs describe the actual graph. The view never infers links or IDs.
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
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  links: TopologyLink[];
}

export type TopologySelection = {
  kind: 'node' | 'link';
  id: string;
} | null;
