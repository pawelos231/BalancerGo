// Large integers stay as decimal strings so JSON round trips preserve every digit.
export type ExactInteger = number | string;

export interface ScenarioLinkConfig {
  bandwidthBitsPerSecond: ExactInteger;
  packetTravelTimeNanoseconds: ExactInteger;
  queueCapacity: ExactInteger;
  lossRate: number;
}

export type ScenarioLinkPatch = Partial<ScenarioLinkConfig>;

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
    snapshotIntervalNanoseconds: ExactInteger;
    seed: ExactInteger;
    speed: number;
    maxEvents: ExactInteger;
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
