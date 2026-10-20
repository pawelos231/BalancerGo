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
  description?: string;
  address?: string;
  active?: boolean;
  metrics: TopologyMetric[];
}

export interface TopologyLink {
  id: string;
  label: string;
  source: string;
  target: string;
  config: {
    bandwidthBitsPerSecond: number;
    packetTravelTimeMs: number;
    queueCapacity: number;
    lossRate: number;
  };
  stats: {
    queuedPackets: number;
    transmitting: boolean;
    travellingPackets: number;
    offeredPackets: number;
    deliveredPackets: number;
    queueDroppedPackets: number;
    lostPackets: number;
    deliveredBytes: number;
  };
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  links: TopologyLink[];
}

export type TopologySelection = { kind: 'node' | 'link'; id: string } | null;
