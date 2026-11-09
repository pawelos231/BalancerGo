import type { ExperimentContext } from '../experiment';

// Large integers stay as decimal strings so JSON round trips preserve every digit.
export type ExactInteger = number | string;

export interface ScenarioLinkConfig {
  bandwidthBitsPerSecond: ExactInteger;
  packetTravelTimeNanoseconds: ExactInteger;
  queueCapacity: ExactInteger;
  lossRate: number;
  queuePolicy?: '' | 'tailDrop' | 'threshold';
  congestionThresholdPercent?: number;
  congestionSignalProbability?: number;
}

// Zero processing time keeps the ideal host; positive time occupies virtual cores.
export interface ScenarioHostConfig {
  cores?: ExactInteger;
  packetProcessingTimeNanoseconds?: ExactInteger;
  queueCapacity?: ExactInteger;
}

export const congestionModeOptions = [
  { value: 'reno', label: 'Reno' },
  { value: 'newReno', label: 'NewReno' },
  { value: 'tahoe', label: 'Tahoe' },
  { value: 'cubic', label: 'CUBIC' },
  { value: 'none', label: 'None (receive window only)' },
] as const;

export type CongestionMode = (typeof congestionModeOptions)[number]['value'];

// The initial window counts segments; the slow-start threshold counts payload bytes.
export interface ScenarioCongestionConfig {
  mode: CongestionMode;
  fastRetransmit: boolean;
  cubicFastConvergence: boolean;
  initialWindowSegments: ExactInteger;
  maximumWindowSegments?: ExactInteger;
  initialSlowStartThresholdBytes: ExactInteger;
}

// RTT-based estimation and timeout backoff can be enabled independently.
export interface ScenarioRTOConfig {
  mode: 'fixed' | 'adaptive';
  exponentialBackoff: boolean;
  initialTimeoutNanoseconds: ExactInteger;
  minimumTimeoutNanoseconds: ExactInteger;
  maximumTimeoutNanoseconds: ExactInteger;
}

export interface ScenarioCloseConfig {
  mslNanoseconds: ExactInteger;
  finWaitTimeoutNanoseconds: ExactInteger;
  maxFINRetries: ExactInteger;
}

// Ordinary data ACKs wait for this packet count or the virtual timer.
export interface ScenarioDelayedACKConfig {
  enabled: boolean;
  segmentsPerACK: ExactInteger;
  maxDelayNanoseconds: ExactInteger;
}

export interface ScenarioStream {
  packets: ExactInteger;
  bytes: ExactInteger;
  packetGapNanoseconds: ExactInteger;
}

export interface ScenarioExecution {
  durationNanoseconds: ExactInteger;
  snapshotIntervalNanoseconds: ExactInteger;
  seed: ExactInteger;
  speed: number;
  maxEvents: ExactInteger;
}

export type { ScenarioV2 as Scenario } from './v2Types.ts';
import type { ScenarioV2 as Scenario } from './v2Types.ts';

export interface ScenarioSummary {
  totalClients: ExactInteger;
  backendCount: ExactInteger;
  clientGroupCount: ExactInteger;
}

export interface ValidatedScenario {
  scenario: Scenario;
  summary: ScenarioSummary;
  resolved?: ResolvedScenario;
}

// Read-only settings after the backend applies profiles and endpoint overrides.
// Client groups stay compact; the native Scenario remains the file and Start format.
export interface ResolvedScenario {
  version: ExactInteger;
  fixed: {
    execution: ScenarioExecution;
    tcp: Record<string, unknown>;
    loadBalancer: { id: string };
    networkDefaults: ScenarioLinkConfig;
    defaultStream: ScenarioStream;
  };
  states: {
    atNanoseconds: ExactInteger;
    loadBalancer: Record<string, unknown>;
    backends: unknown[];
    clientGroups: unknown[];
    totalClients: ExactInteger;
  }[];
  totalClients: ExactInteger;
}

// Both directions have concrete values, with no inherited patches left to apply.
export interface ResolvedLinks {
  toLoadBalancer: ScenarioLinkConfig;
  fromLoadBalancer: ScenarioLinkConfig;
}

export interface SimulationReceipt {
  runId: string;
  status: 'accepted';
}

export interface CurrentSimulation {
  runId: string;
  status: 'accepted' | 'running' | 'finished' | 'failed';
  scenario: Scenario;
  experiment: ExperimentContext;
}
