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

export type ScenarioLinkPatch = Partial<ScenarioLinkConfig>;

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

export interface ScenarioEndpointNetwork {
  profile?: string;
  toLoadBalancer: ScenarioLinkPatch;
  fromLoadBalancer: ScenarioLinkPatch;
}

export interface ScenarioStream {
  packets: ExactInteger;
  bytes: ExactInteger;
  packetGapNanoseconds: ExactInteger;
}

export interface ScenarioClientGroup {
  id: string;
  count: ExactInteger;
  startAtNanoseconds: ExactInteger;
  clientGapNanoseconds: ExactInteger;
  network: ScenarioEndpointNetwork;
  stream: Partial<ScenarioStream>;
  overrides?: {
    index: ExactInteger;
    network: ScenarioEndpointNetwork;
    stream: Partial<ScenarioStream>;
  }[];
}

// Field names mirror the native backend document; durations are exact nanoseconds.
export interface Scenario {
  version: ExactInteger;
  execution: {
    durationNanoseconds: ExactInteger;
    // Real time between UI snapshots; independent of the virtual clock.
    snapshotIntervalNanoseconds: ExactInteger;
    seed: ExactInteger;
    speed: number;
    maxEvents: ExactInteger;
  };
  tcp?: {
    receiveBufferBytes?: ExactInteger;
    sackEnabled?: boolean;
    ECNenabled?: boolean;
    delayedAck?: Partial<ScenarioDelayedACKConfig>;
    congestionControl?: Partial<ScenarioCongestionConfig>;
    rto?: Partial<ScenarioRTOConfig>;
    close?: Partial<ScenarioCloseConfig>;
    clientIdleTimeoutNanoseconds?: ExactInteger;
    backendHalfOpenIdleTimeoutNanoseconds?: ExactInteger;
    backendEstablishedIdleTimeoutNanoseconds?: ExactInteger;
  };
  network: {
    defaults: ScenarioLinkConfig;
    profiles?: { id: string; config: ScenarioLinkPatch }[];
  };
  resources: {
    loadBalancer: {
      id: string;
      algorithm: string;
      ingress: ScenarioLinkConfig;
      forwardingBitsPerSecond: ExactInteger;
      halfOpenIdleTimeoutNanoseconds?: ExactInteger;
      establishedIdleTimeoutNanoseconds?: ExactInteger;
      closingIdleTimeoutNanoseconds?: ExactInteger;
      resetIdleTimeoutNanoseconds?: ExactInteger;
    };
    backends: {
      id: string;
      name?: string;
      address: string;
      port: ExactInteger;
      network: ScenarioEndpointNetwork;
    }[];
  };
  workload: {
    defaults: ScenarioStream;
    clientGroups: ScenarioClientGroup[];
  };
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
  version: ExactInteger;
  execution: Scenario['execution'];
  tcp: {
    receiveBufferBytes: number;
    sackEnabled: boolean;
    ECNenabled: boolean;
    delayedAck: ScenarioDelayedACKConfig;
    congestionControl: ScenarioCongestionConfig;
    rto: ScenarioRTOConfig;
    close: ScenarioCloseConfig;
    clientIdleTimeoutNanoseconds: ExactInteger;
    backendHalfOpenIdleTimeoutNanoseconds: ExactInteger;
    backendEstablishedIdleTimeoutNanoseconds: ExactInteger;
  };
  networkDefaults: ScenarioLinkConfig;
  loadBalancer: Scenario['resources']['loadBalancer'] & {
    halfOpenIdleTimeoutNanoseconds: ExactInteger;
    establishedIdleTimeoutNanoseconds: ExactInteger;
    closingIdleTimeoutNanoseconds: ExactInteger;
    resetIdleTimeoutNanoseconds: ExactInteger;
  };
  totalClients: ExactInteger;
  defaultStream: ScenarioStream;
  backends: {
    id: string;
    name: string;
    address: string;
    port: ExactInteger;
    network: ResolvedLinks;
  }[];
  clientGroups: {
    id: string;
    count: ExactInteger;
    startAtNanoseconds: ExactInteger;
    clientGapNanoseconds: ExactInteger;
    network: ResolvedLinks;
    stream: ScenarioStream;
    overrides: {
      index: ExactInteger;
      network: ResolvedLinks;
      stream: ScenarioStream;
    }[];
  }[];
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
}
