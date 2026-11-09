import type {
  ExactInteger,
  ScenarioExecution,
  ScenarioCloseConfig,
  ScenarioCongestionConfig,
  ScenarioDelayedACKConfig,
  ScenarioHostConfig,
  ScenarioLinkConfig,
  ScenarioRTOConfig,
  ScenarioStream,
} from './types.ts';

// Go pointer fields accept omission or null; explicit zero remains an override.
export type NullablePatch<T> = { [Key in keyof T]?: T[Key] | null };
export type V2LinkPatch = NullablePatch<ScenarioLinkConfig>;
export type V2HostConfig = NullablePatch<ScenarioHostConfig>;
export type V2StreamPatch = NullablePatch<ScenarioStream>;

// Fixed settings stay constant; state keys are exact decimal virtual nanoseconds.
// The same document is used by files, the form and runtime admission.
export interface ScenarioV2 {
  version: ExactInteger;
  fixed: {
    execution: ScenarioExecution;
    tcp: V2TCPConfig;
    loadBalancer: { id: string };
    network: V2NetworkConfig;
    workload: { defaults: ScenarioStream };
  };
  states: Record<string, ScenarioStateV2>;
}

// Every state is complete; omitted entities are removed, not inherited.
export interface ScenarioStateV2 {
  resources: {
    loadBalancer: LoadBalancerStateV2;
    backends: BackendStateV2[];
  };
  workload: { clientGroups: V2ClientGroup[] };
}

// These defaults and profiles apply independently in every state.
export interface V2NetworkConfig {
  defaults: ScenarioLinkConfig;
  profiles: { id: string; config: V2LinkPatch }[];
}

export interface V2EndpointNetwork {
  profile: string;
  toLoadBalancer: V2LinkPatch;
  fromLoadBalancer: V2LinkPatch;
}

// Algorithm changes affect new mappings; idle limits also apply to existing ones.
export interface LoadBalancerStateV2 {
  algorithm: string;
  host?: V2HostConfig;
  ingress: ScenarioLinkConfig;
  forwardingBitsPerSecond: ExactInteger;
  mappingTimeouts: V2MappingTimeouts;
}

export interface V2MappingTimeouts {
  halfOpenIdleTimeoutNanoseconds?: ExactInteger | null;
  establishedIdleTimeoutNanoseconds?: ExactInteger | null;
  closingIdleTimeoutNanoseconds?: ExactInteger | null;
  resetIdleTimeoutNanoseconds?: ExactInteger | null;
}

// Availability describes the backend itself; health observed by the LB is runtime state.
export interface BackendStateV2 {
  id: string;
  name?: string;
  address: string;
  port: ExactInteger;
  network: V2EndpointNetwork;
  host?: V2HostConfig;
  physicalAvailable?: boolean | null;
  administrativeState?: 'active' | 'drain' | null;
}

// Count is a lifetime birth budget. Existing clients survive a smaller budget.
export interface V2ClientGroup {
  id: string;
  count: ExactInteger;
  startAtNanoseconds: ExactInteger;
  clientGapNanoseconds: ExactInteger;
  network: V2EndpointNetwork;
  stream: V2StreamPatch;
  overrides?: {
    index: ExactInteger;
    network: V2EndpointNetwork;
    stream: V2StreamPatch;
  }[];
}

// TCP remains fixed for a run; nested settings fill omitted fields from model defaults.
export interface V2TCPConfig {
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
