import type { Scenario, ScenarioClientGroup } from './types.ts';

export interface ScenarioPreset {
  id: string;
  title: string;
  description: string;
  build: (defaults: Scenario) => Scenario;
}

const fastBitsPerSecond = 1_000_000_000;
const slowBitsPerSecond = 10_000_000;

// Network presets share duration and payload; remaining settings start from Go defaults.
function smallExperiment(defaults: Scenario): Scenario {
  const scenario = structuredClone(defaults);
  scenario.execution.durationNanoseconds = 180_000_000_000;
  scenario.execution.speed = 1;
  scenario.network.defaults.bandwidthBitsPerSecond = fastBitsPerSecond;
  scenario.network.profiles = [];
  scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond = fastBitsPerSecond;
  scenario.resources.loadBalancer.forwardingBitsPerSecond = fastBitsPerSecond;
  scenario.resources.backends = [1, 2].map((index) => ({
    id: `backend-${index}`,
    name: `Backend ${index}`,
    address: `10.0.0.${index}`,
    port: 80,
    network: { toLoadBalancer: {}, fromLoadBalancer: {} },
  }));
  scenario.workload.defaults.bytes = 1_073_741_824;
  scenario.workload.defaults.packetGapNanoseconds = 0;
  scenario.workload.clientGroups = [clientGroup('clients', 16)];
  return scenario;
}

function clientGroup(id: string, count: number, profile?: string): ScenarioClientGroup {
  return {
    id,
    count,
    startAtNanoseconds: 0,
    clientGapNanoseconds: 0,
    network: { profile, toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: {},
  };
}

// Very high bitrates expose the engine's one-nanosecond serialization floor.
function throughputExperiment(defaults: Scenario): Scenario {
  const scenario = smallExperiment(defaults);
  scenario.execution = {
    durationNanoseconds: 5_000_000,
    snapshotIntervalNanoseconds: 50_000,
    seed: 1,
    speed: 0,
    maxEvents: 50_000_000,
  };
  scenario.network.defaults = {
    bandwidthBitsPerSecond: 80_000_000_000_000,
    packetTravelTimeNanoseconds: 0,
    queueCapacity: 4096,
    lossRate: 0,
  };
  scenario.network.profiles = [{
    id: 'throughput-backend',
    config: { bandwidthBitsPerSecond: 800_000_000_000_000 },
  }];
  scenario.resources.loadBalancer.algorithm = 'roundRobin';
  scenario.resources.loadBalancer.forwardingBitsPerSecond = 8_000_000_000_000_000;
  scenario.resources.loadBalancer.ingress = {
    bandwidthBitsPerSecond: 8_000_000_000_000_000,
    packetTravelTimeNanoseconds: 0,
    queueCapacity: 4096,
    lossRate: 0,
  };
  scenario.resources.backends = Array.from({ length: 5 }, (_, index) => ({
    id: `backend-${index + 1}`,
    name: `Backend ${index + 1}`,
    address: `10.0.0.${index + 1}`,
    port: 80,
    network: { profile: 'throughput-backend', toLoadBalancer: {}, fromLoadBalancer: {} },
  }));
  scenario.workload.clientGroups = [clientGroup('clients', 50)];
  return scenario;
}

// Add a factory here to extend the library without copying backend defaults into the UI.
export const scenarioPresets: readonly ScenarioPreset[] = [
  {
    id: 'mixed-clients',
    title: 'Mixed client links',
    description: 'Two groups of 8 clients: 10 Mbit/s and 1 Gbit/s access links. Two fast backends.',
    build(defaults) {
      const scenario = smallExperiment(defaults);
      scenario.network.profiles = [{
        id: 'slow-client',
        config: { bandwidthBitsPerSecond: slowBitsPerSecond },
      }];
      scenario.workload.clientGroups = [
        clientGroup('slow-clients', 8, 'slow-client'),
        clientGroup('fast-clients', 8),
      ];
      return scenario;
    },
  },
  {
    id: 'mixed-backends',
    title: 'Heterogeneous backends',
    description: '16 fast clients and two backends with 10 Mbit/s and 1 Gbit/s links.',
    build(defaults) {
      const scenario = smallExperiment(defaults);
      scenario.network.profiles = [{
        id: 'slow-backend',
        config: { bandwidthBitsPerSecond: slowBitsPerSecond },
      }];
      scenario.resources.backends[0].network.profile = 'slow-backend';
      return scenario;
    },
  },
  {
    id: 'ingress-bottleneck',
    title: 'Shared input bottleneck',
    description: '32 fast clients share a 10 Mbit/s LB receiver. Output budget stays at 1 Gbit/s.',
    build(defaults) {
      const scenario = smallExperiment(defaults);
      scenario.workload.clientGroups = [clientGroup('clients', 32)];
      scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond = slowBitsPerSecond;
      return scenario;
    },
  },
  {
    id: 'egress-bottleneck',
    title: 'Shared output bottleneck',
    description: '32 fast clients share a 10 Mbit/s forwarding budget. Input limit stays at 1 Gbit/s.',
    build(defaults) {
      const scenario = smallExperiment(defaults);
      scenario.workload.clientGroups = [clientGroup('clients', 32)];
      scenario.resources.loadBalancer.forwardingBitsPerSecond = slowBitsPerSecond;
      return scenario;
    },
  },
  {
    id: 'simulator-throughput',
    title: 'Simulator throughput',
    description:
      '50 clients at 10 TB/s, 5 backends at 100 TB/s, and shared LB limits of 1 PB/s. ' +
      'Runs unpaced for 5 virtual ms; packet transmission reaches the 1 ns engine floor.',
    build: throughputExperiment,
  },
];
