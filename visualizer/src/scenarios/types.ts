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

// Go pointer fields accept omission or null; explicit zero remains an override.
export type NullablePatch<T> = { [Key in keyof T]?: T[Key] | null };
export type ScenarioLinkPatch = NullablePatch<ScenarioLinkConfig>;
export type ScenarioHostPatch = NullablePatch<ScenarioHostConfig>;
export type ScenarioStreamPatch = NullablePatch<ScenarioStream>;

// Fixed settings stay constant; state keys are exact decimal virtual nanoseconds.
// The same document is used by files, the form and runtime admission.
export interface Scenario {
  scenarioVersion: ExactInteger;
  fixed: {
    execution: ScenarioExecution;
    tcp: ScenarioTCPConfig;
    loadBalancer: { id: string };
    network: ScenarioNetworkConfig;
    workload: { defaults: ScenarioStream };
  };
  states: Record<string, ScenarioState>;
}

// Every state is complete; omitted entities are removed, not inherited.
export interface ScenarioState {
  resources: {
    loadBalancer: ScenarioLoadBalancerState;
    backends: ScenarioBackendState[];
  };
  workload: { clientGroups: ScenarioClientGroup[] };
}

// These defaults and profiles apply independently in every state.
export interface ScenarioNetworkConfig {
  defaults: ScenarioLinkConfig;
  profiles: { id: string; config: ScenarioLinkPatch }[];
}

export interface ScenarioEndpointNetwork {
  profile: string;
  toLoadBalancer: ScenarioLinkPatch;
  fromLoadBalancer: ScenarioLinkPatch;
}

// Algorithm changes affect new mappings; idle limits also apply to existing ones.
export interface ScenarioLoadBalancerState {
  algorithm: string;
  host?: ScenarioHostPatch;
  ingress: ScenarioLinkConfig;
  forwardingBitsPerSecond: ExactInteger;
  mappingTimeouts: ScenarioMappingTimeouts;
}

export interface ScenarioMappingTimeouts {
  halfOpenIdleTimeoutNanoseconds?: ExactInteger | null;
  establishedIdleTimeoutNanoseconds?: ExactInteger | null;
  closingIdleTimeoutNanoseconds?: ExactInteger | null;
  resetIdleTimeoutNanoseconds?: ExactInteger | null;
}

// Availability describes the backend itself; health observed by the LB is runtime state.
export interface ScenarioBackendState {
  id: string;
  name?: string;
  address: string;
  port: ExactInteger;
  network: ScenarioEndpointNetwork;
  host?: ScenarioHostPatch;
  physicalAvailable?: boolean | null;
  administrativeState?: 'active' | 'drain' | null;
}

// Count is a lifetime birth budget. Existing clients survive a smaller budget.
export interface ScenarioClientGroup {
  id: string;
  count: ExactInteger;
  startAtNanoseconds: ExactInteger;
  clientGapNanoseconds: ExactInteger;
  network: ScenarioEndpointNetwork;
  stream: ScenarioStreamPatch;
  overrides?: {
    index: ExactInteger;
    network: ScenarioEndpointNetwork;
    stream: ScenarioStreamPatch;
  }[];
}

// TCP remains fixed for a run; nested settings fill omitted fields from model defaults.
export interface ScenarioTCPConfig {
  mssBytes?: ExactInteger | null;
  receiveBufferBytes?: ExactInteger | null;
  sackEnabled?: boolean | null;
  ECNenabled?: boolean | null;
  delayedAck?: NullablePatch<ScenarioDelayedACKConfig>;
  rto?: NullablePatch<ScenarioRTOConfig>;
  close?: NullablePatch<ScenarioCloseConfig>;
  congestionControl?: NullablePatch<ScenarioCongestionConfig>;
  clientIdleTimeoutNanoseconds?: ExactInteger | null;
  backendHalfOpenIdleTimeoutNanoseconds?: ExactInteger | null;
  backendEstablishedIdleTimeoutNanoseconds?: ExactInteger | null;
}

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
  scenarioVersion: ExactInteger;
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
