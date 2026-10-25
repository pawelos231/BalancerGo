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
  AcknowledgedPayloadBytes?: number;
  Transfer?: ClientTransfer;
}

export type TransferFailureReason =
  | 'synRetryLimit'
  | 'dataRetryLimit'
  | 'readTimeout'
  | 'remoteClosed'
  | 'closedEarly'
  | 'interrupted'
  | 'balancerTimeout'
  | 'backendRemoved';

export interface ClientTransfer {
  status: 'inProgress' | 'succeeded' | 'failed';
  failureReason?: TransferFailureReason;
  elapsedSeconds: number;
  handshakeCompleted: boolean;
  handshakeDurationSeconds: number;
  payloadDurationSeconds: number;
  synRetransmissions: number;
  dataRetransmissions: number;
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
  ReceivedPayloadBytes?: number;
}

export interface ServerStateWithClients {
  server: ServerState;
  clients: ClientState[];
}

export interface LoadBalancerState {
  mode?: string;
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

// Form field names remain shared with the native scenario adapter; these are not live settings.
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
  algorithm: AlgorithmName;
  virtualTimeSeconds: number;
  error?: string;
  performance?: SimulationPerformanceMetrics;
  payload?: SimulationPayloadProgress;
  transfers?: SimulationTransferResults;
}

// Zero samples means unavailable; measured zero durations remain valid samples.
export interface DurationSummary {
  samples: number;
  meanSeconds: number;
  minSeconds: number;
  maxSeconds: number;
}

export interface SimulationTransferResults {
  started: number;
  notStarted: number;
  succeeded: number;
  failed: number;
  inProgress: number;
  failures: Partial<Record<TransferFailureReason, number>>;
  synRetransmissions: number;
  dataRetransmissions: number;
  handshake: DurationSummary;
  payload: DurationSummary;
  completion: DurationSummary;
}

export interface SimulationPayloadProgress {
  receivedBytes: number;
  acknowledgedBytes: number;
}

// Session averages measure simulator execution over real time, independently of virtual limits.
export interface SimulationPerformanceMetrics {
  wallTimeSeconds: number;
  eventsExecuted: number;
  eventsPerWallSecond: number;
  forwardedPackets: number;
  forwardedPacketsPerWallSecond: number;
}

export interface CommandError {
  type: 'commandError';
  commandType: CommandType;
  error: string;
}
