import type { Scenario, ScenarioClientGroup } from '../types.ts';

// Each endpoint receives independent directional patches and its own resource ID.
export function presetBackends(count: number): Scenario['states'][string]['resources']['backends'] {
  return Array.from({ length: count }, (_, index) => ({
    id: `backend-${index + 1}`,
    name: `Backend ${index + 1}`,
    address: `10.0.0.${index + 1}`,
    port: 80,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  }));
}

// An unpatched group inherits both access links and the default stream.
export function presetClientGroup(id: string, count: number): ScenarioClientGroup {
  return {
    id,
    count,
    startAtNanoseconds: 0,
    clientGapNanoseconds: 0,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: {},
  };
}

// New experiments reset inherited settings so only their stated variable changes.
export function isolatedExperiment(defaults: Scenario): Scenario {
  const scenario = structuredClone(defaults);
  if (scenario.fixed.tcp) {
    delete scenario.fixed.tcp.mssBytes;
    delete scenario.fixed.tcp.ECNenabled;
    delete scenario.fixed.tcp.delayedAck;
    if (scenario.fixed.tcp.congestionControl) delete scenario.fixed.tcp.congestionControl.maximumWindowSegments;
  }

  scenario.scenarioVersion = 2;
  scenario.fixed.loadBalancer = { id: 'lb' };
  scenario.fixed.execution = {
    durationNanoseconds: 180_000_000_000,
    snapshotIntervalNanoseconds: 100_000_000,
    seed: 1,
    speed: 1,
    maxEvents: 50_000_000,
  };
  scenario.fixed.network = {
    defaults: {
      bandwidthBitsPerSecond: 1_000_000_000,
      packetTravelTimeNanoseconds: 1_000_000,
      queueCapacity: 1024,
      lossRate: 0,
    },
    profiles: [],
  };
  scenario.states = {
    '0': {
      resources: {
        loadBalancer: {
          algorithm: 'roundRobin',
          ingress: {
            bandwidthBitsPerSecond: 1_000_000_000,
            packetTravelTimeNanoseconds: 0,
            queueCapacity: 1024,
            lossRate: 0,
          },
          forwardingBitsPerSecond: 1_000_000_000,
          mappingTimeouts: {},
        },
        backends: presetBackends(2),
      },
      workload: { clientGroups: [presetClientGroup('clients', 16)] },
    },
  };
  scenario.fixed.workload = {
    defaults: {
      packets: 10_000,
      bytes: 1_073_741_824,
      packetGapNanoseconds: 0,
    },
  };

  return scenario;
}
