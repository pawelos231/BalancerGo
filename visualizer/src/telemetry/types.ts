import type { ClientConnection, ClientKey, ClientTransfer, SimulationMetadata } from '../types';
import type {
  TopologyLinkConfig,
  TopologyLinkStats,
  TopologyTraffic,
} from '../topology/types';

// These are numeric live-view projections, not the exact archive representation.
// Results uses telemetrySource() before hydration to retain original integer digits.
// HTTP carries resource identities and settings only when the topology changes.
export interface MetricDefinition {
  key: string;
  label: string;
  unit?: string;
  value?: number;
}

export interface NodeDefinition {
  id: string;
  kind: string;
  label: string;
  resourceId?: string;
  groupId?: string;
  description?: string;
  address?: string;
  metrics: MetricDefinition[];
  clientKey?: ClientKey;
}

export interface LinkDefinition {
  id: string;
  label: string;
  source: string;
  target: string;
  config: TopologyLinkConfig;
}

export interface IngressDefinition {
  config: TopologyLinkConfig;
  queues: {
    id: string;
    side: 'clients' | 'backends';
    label: string;
    capacity: number;
  }[];
}

export interface ServerDefinition {
  id: string;
  name: string;
  address: string;
  port: number;
}

export interface TopologyDefinition {
  runId: string;
  revision: string;
  nodes: NodeDefinition[];
  links: LinkDefinition[];
  ingress?: IngressDefinition;
  servers: ServerDefinition[];
}

// Each frame is a complete current measurement, independent of skipped frames.
export interface NodeTelemetry {
  id: string;
  active?: boolean;
  tcpPhase?: string;
  tcpState?: string;
  closeOutcome?: string;
  metrics: Record<string, number>;
  traffic?: TopologyTraffic;
}

export interface ServerTelemetry {
  id: string;
  active: boolean;
  activeConnections: number;
  halfOpenConnections: number;
  latencyP95Milli: number;
  numberOfHandledPackets: number;
  receivedPayloadBytes?: number;
  cpuUsage?: number;
  clients: {
    id: string;
    retrySyn: number;
    retryData: number;
    acknowledgedPayloadBytes?: number;
    transfer?: ClientTransfer;
    connection?: ClientConnection;
  }[];
}

export interface SimulationSnapshot {
  type: 'simulationSnapshot';
  snapshotVersion: 1;
  simulation: SimulationMetadata;
  mode: string;
  algorithm: string;
  servers: ServerTelemetry[];
  topology: {
    revision: string;
    nodes: NodeTelemetry[];
    links: { id: string; stats: TopologyLinkStats }[];
    ingress?: {
      stats: TopologyLinkStats;
      queues: { id: string; stats: TopologyLinkStats }[];
    };
  };
}

// End events carry their own topology: current HTTP may already describe the next run.
export interface SimulationEnded {
  type: 'simulationEnded';
  runId: string;
  reason: 'finished' | 'failed' | 'stopped';
  commandId?: string;
  finalSnapshot: SimulationSnapshot | null;
  topologyDefinition: TopologyDefinition | null;
}
