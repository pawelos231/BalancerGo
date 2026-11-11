import type { CommandType } from './commands';
import type { ExperimentContext } from './experiment';
import type { TopologyGraph } from './topology/types';
import type { SimulationSnapshot, TopologyDefinition } from './telemetry/types';

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
  Connection?: ClientConnection;
}

export type TCPState =
  | 'closed' | 'synSent' | 'synReceived' | 'established'
  | 'finWait1' | 'finWait2' | 'closeWait' | 'closing' | 'lastAck' | 'timeWait';

export type CloseOutcome = 'graceful' | 'reset' | 'finRetryLimit' | 'finWaitTimeout' | 'aborted';

// Transport teardown can fail after the payload has already succeeded.
export interface ClientConnection {
  state: TCPState;
  outcome?: CloseOutcome;
  finRetransmissions: number;
  elapsedNanoseconds: number;
}

export type TransferFailureReason =
  | 'synRetryLimit'
  | 'dataRetryLimit'
  | 'readTimeout'
  | 'remoteClosed'
  | 'closedEarly'
  | 'interrupted'
  | 'balancerTimeout'
  | 'backendRemoved'
  | 'connectionReset'
  | 'closeTimeout';

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

// Model utilization uses virtual core time, separate from the simulator's real CPU use.
export interface HostState {
  enabled: boolean;
  cores: number;
  busyCores: number;
  queueCapacity: number;
  queuedPackets: number;
  maxQueuedPackets: number;
  processedPackets: number;
  queueDroppedPackets: number;
  utilization: number;
  busyTimeSeconds?: number;
  availableCoreTimeSeconds?: number;
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

export type SimulationCommand = {
  Type: CommandType.StopSimulation;
  Payload: Record<string, never>;
};

// Canonical positive uint64 decimal text; never convert publication IDs to Number.
export type PublicationSequence = string;

export type SimulationMetadata = {
  algorithm: AlgorithmName;
  virtualTimeSeconds: number;
  error?: string;
  performance?: SimulationPerformanceMetrics;
  payload?: SimulationPayloadProgress;
  transfers?: SimulationTransferResults;
} & (
  | { status: 'idle'; runId?: string; experiment?: never; publicationSequence?: never }
  | {
      status: 'running' | 'finished' | 'failed';
      runId: string;
      experiment: ExperimentContext;
      publicationSequence: PublicationSequence;
    }
);

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

export interface StopCommandResult {
  type: 'commandResult';
	commandType: CommandType.StopSimulation;
  runId: string;
  commandId: string;
  status: 'applied' | 'rejected';
  error?: string;
  finalSnapshot?: SimulationSnapshot | null;
  topologyDefinition?: TopologyDefinition | null;
}
