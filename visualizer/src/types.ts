import type { CommandType } from './commands';
import type { TopologyGraph } from './topology/types';

export interface ClientKey {
  SrcIP: string;
  SrcPort: number;
  DstIP: string;
  DstPort: number;
  Protocol: string;
}

export interface ClientState {
  Key: ClientKey;
  RetrySyn: number;
  RetryData: number;
}

export interface ServerState {
  ID: string;
  Name: string;
  Address: string;
  Port: number;
  Active: boolean;
  ActiveConnections: number;
  HalfOpenConnections: number;
  LatencyP95Milli: number;
  NumberOfHandledPackets: number;
}

export interface ServerStateWithClients {
  server: ServerState;
  clients: ClientState[];
}

export interface LoadBalancerState {
  algorithm: string;
  servers: ServerStateWithClients[];
  simulation: SimulationMetadata;
  topology?: TopologyGraph;
}

export type AlgorithmName = 'roundRobin' | 'random';

export type SimulationCommand =
  | {
      Type: CommandType.SetAlgorithm;
      Payload: { Name: string };
    }
  | {
      Type: CommandType.SpawnClient;
      Payload: { Count: number; groupId?: string };
    }
  | {
      Type: CommandType.StopSimulation;
      Payload: Record<string, never>;
    };

// Compact settings retained in live snapshots; editable scenarios use scenarios/types.ts.
export interface SimulationConfig {
  clients: number;
  servers: number;
  packets: number;
  bytes: number;
  durationSeconds: number;
  clientGapMs: number;
  packetGapMs: number;
  snapshotIntervalMs: number;
  bandwidthBitsPerSecond: number;
  forwardingBitsPerSecond: number;
  ingressBandwidthBitsPerSecond: number;
  ingressPacketTravelTimeMs: number;
  ingressQueueCapacity: number;
  ingressLoss: number;
  packetTravelTimeMs: number;
  linkQueue: number;
  loss: number;
  seed: number;
  algorithm: AlgorithmName;
  speed: number;
  maxEvents: number;
}

export interface SimulationMetadata {
  runId?: string;
  status: 'idle' | 'running' | 'finished' | 'failed';
  config: SimulationConfig;
  virtualTimeSeconds: number;
  error?: string;
}

export interface CommandError {
  type: 'commandError';
  commandType: CommandType;
  error: string;
}
