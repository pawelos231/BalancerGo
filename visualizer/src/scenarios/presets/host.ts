import type { ScenarioV2 } from '../v2Types.ts';
import type { ScenarioHostConfig } from '../types.ts';
import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment } from './base.ts';
import { idealHost } from '../host.ts';

// These are assumed service budgets for experiments, not profiles of real CPUs.
function cpu(cores: number, microseconds: number, queueCapacity = 128): ScenarioHostConfig {
  return { cores, packetProcessingTimeNanoseconds: microseconds * 1000, queueCapacity };
}

function hostExperiment(defaults: ScenarioV2): ScenarioV2 {
  const scenario = isolatedExperiment(defaults);
  scenario.states['0'].resources.loadBalancer.host = idealHost();
  for (const backend of scenario.states['0'].resources.backends) backend.host = idealHost();
  scenario.fixed.network.defaults.bandwidthBitsPerSecond = 10_000_000_000;
  scenario.fixed.network.defaults.packetTravelTimeNanoseconds = 10_000;
  scenario.fixed.network.defaults.queueCapacity = 4096;
  scenario.states['0'].resources.loadBalancer.ingress.bandwidthBitsPerSecond = 10_000_000_000;
  scenario.states['0'].resources.loadBalancer.ingress.queueCapacity = 4096;
  scenario.states['0'].resources.loadBalancer.forwardingBitsPerSecond = 10_000_000_000;
  scenario.states['0'].workload.clientGroups[0].count = 64;
  scenario.fixed.execution.maxEvents = 5_000_000_000;
  // Repair multiple losses promptly so CPU experiments can sustain their offered load.
  scenario.fixed.tcp = {
    mssBytes: 1460,
    receiveBufferBytes: 65535,
    sackEnabled: true,
    ECNenabled: false,
    delayedAck: { enabled: false },
    clientIdleTimeoutNanoseconds: 0,
    backendHalfOpenIdleTimeoutNanoseconds: 0,
    backendEstablishedIdleTimeoutNanoseconds: 0,
    congestionControl: {
      mode: 'newReno', fastRetransmit: true,
      initialWindowSegments: 4, initialSlowStartThresholdBytes: 65535,
    },
  };
  return scenario;
}

export const hostPresets: readonly ScenarioPreset[] = [
  {
    id: 'host-cpu-disabled', category: 'host-cpu', title: 'Host CPU disabled reference',
    description: '64 NewReno clients with SACK and fast links, with zero CPU processing cost on the LB and both backends. ' +
      'Reference for the CPU presets.',
    build: hostExperiment,
  },
  {
    id: 'lb-cpu-bottleneck', category: 'host-cpu', title: 'Load balancer CPU bottleneck',
    description: 'The LB has one virtual core, 10 µs per packet and 128 waiting slots. ' +
      'Data and returning ACKs share its CPU; backend CPU is disabled.',
    build(defaults) {
      const scenario = hostExperiment(defaults);
      scenario.states['0'].resources.loadBalancer.host = cpu(1, 10);
      return scenario;
    },
  },
  {
    id: 'backend-cpu-bottleneck', category: 'host-cpu', title: 'Backend CPU bottleneck',
    description: 'Each backend has one virtual core, 10 µs per packet and 128 waiting slots. ' +
      'Fast links and an ideal LB expose receiver processing limits.',
    build(defaults) {
      const scenario = hostExperiment(defaults);
      for (const backend of scenario.states['0'].resources.backends) backend.host = cpu(1, 10);
      return scenario;
    },
  },
  {
    id: 'heterogeneous-backend-cpus', category: 'host-cpu', title: 'Heterogeneous backend CPUs',
    description: 'Equal links and Round Robin, but backend 1 has one core at 100 µs/packet ' +
      'and backend 2 has four cores at 10 µs/packet.',
    build(defaults) {
      const scenario = hostExperiment(defaults);
      scenario.states['0'].resources.backends[0].host = cpu(1, 100);
      scenario.states['0'].resources.backends[1].host = cpu(4, 10);
      return scenario;
    },
  },
  {
    id: 'small-backend-cpu-queue', category: 'host-cpu', title: 'Small backend CPU queues',
    description: 'Each backend has one core at 10 µs/packet but only eight waiting slots. ' +
      'CPU queue overflow drops packets independently of network queues.',
    build(defaults) {
      const scenario = hostExperiment(defaults);
      for (const backend of scenario.states['0'].resources.backends) backend.host = cpu(1, 10, 8);
      return scenario;
    },
  },
  {
    id: 'lb-cpu-four-cores', category: 'host-cpu', title: 'Load balancer CPU: four cores',
    description: 'The same LB processing cost of 10 µs/packet with four virtual cores. ' +
      'Compare against the one-core LB CPU bottleneck preset.',
    build(defaults) {
      const scenario = hostExperiment(defaults);
      scenario.states['0'].resources.loadBalancer.host = cpu(4, 10);
      return scenario;
    },
  },
];
