import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import type { Scenario } from '../types.ts';
import { idealHost } from '../host.ts';
import { isolatedExperiment, presetClientGroup } from './base.ts';

const overloadAt = '30000000000';
const recoveryAt = '90000000000';

// Finite streams outlast both transitions, even at the initial shared LB rate.
function timelineExperiment(defaults: Scenario): Scenario {
  const scenario = isolatedExperiment(defaults);
  scenario.fixed.execution.maxEvents = 5_000_000_000;
  scenario.fixed.tcp = {
    mssBytes: 1460,
    receiveBufferBytes: 65535,
    sackEnabled: true,
    ECNenabled: false,
    delayedAck: { enabled: false, segmentsPerACK: 2, maxDelayNanoseconds: 40_000_000 },
    rto: {
      mode: 'adaptive', exponentialBackoff: true,
      initialTimeoutNanoseconds: 1_000_000_000,
      minimumTimeoutNanoseconds: 200_000_000,
      maximumTimeoutNanoseconds: 60_000_000_000,
    },
    close: { mslNanoseconds: 30_000_000_000, finWaitTimeoutNanoseconds: 60_000_000_000, maxFINRetries: 3 },
    congestionControl: {
      mode: 'newReno', fastRetransmit: true, cubicFastConvergence: true,
      initialWindowSegments: 4, maximumWindowSegments: 0, initialSlowStartThresholdBytes: 65535,
    },
    clientIdleTimeoutNanoseconds: 0,
    backendHalfOpenIdleTimeoutNanoseconds: 0,
    backendEstablishedIdleTimeoutNanoseconds: 0,
  };
  scenario.fixed.network.defaults = {
    bandwidthBitsPerSecond: 2_000_000_000,
    packetTravelTimeNanoseconds: 10_000,
    queueCapacity: 4096,
    lossRate: 0,
    queuePolicy: 'tailDrop',
    congestionThresholdPercent: 0,
    congestionSignalProbability: 0,
  };
  scenario.fixed.network.profiles = [
    { id: 'slow-access', config: { bandwidthBitsPerSecond: 20_000_000 } },
    { id: 'slower-backend', config: { bandwidthBitsPerSecond: 1_000_000_000 } },
  ];

  const state = scenario.states['0'];
  state.resources.loadBalancer.host = idealHost();
  state.resources.loadBalancer.ingress = {
    ...scenario.fixed.network.defaults,
    bandwidthBitsPerSecond: 1_000_000_000,
    packetTravelTimeNanoseconds: 0,
  };
  state.resources.loadBalancer.mappingTimeouts = {
    halfOpenIdleTimeoutNanoseconds: 0,
    establishedIdleTimeoutNanoseconds: 0,
    closingIdleTimeoutNanoseconds: 0,
    resetIdleTimeoutNanoseconds: 0,
  };
  for (const backend of state.resources.backends) backend.host = idealHost();
  state.resources.backends[0].network.profile = 'slower-backend';

  const slow = presetClientGroup('slow-clients', 16);
  slow.network.profile = 'slow-access';
  state.workload.clientGroups = [slow, presetClientGroup('fast-clients', 16)];
  return scenario;
}

export const timelinePresets: readonly ScenarioPreset[] = [
  {
    id: 'timeline-arrival-burst', category: 'timeline', title: 'Timeline: burst, then stop arrivals',
    description: '8 slow baseline clients; at 30 s add 24 clients in a burst and a 120-client lifetime budget at one arrival/s. ' +
      'At 90 s remove the added generators; clients already born keep transferring.',
    build(defaults) {
      const scenario = timelineExperiment(defaults);
      const initial = scenario.states['0'];
      initial.workload.clientGroups = [presetClientGroup('baseline', 8)];
      initial.workload.clientGroups[0].network.profile = 'slow-access';

      const overload = structuredClone(initial);
      const burst = presetClientGroup('burst', 24);
      burst.startAtNanoseconds = Number(overloadAt);
      const arrivals = presetClientGroup('arrivals', 120);
      arrivals.startAtNanoseconds = Number(overloadAt);
      arrivals.clientGapNanoseconds = 1_000_000_000;
      overload.workload.clientGroups.push(burst, arrivals);

      scenario.states[overloadAt] = overload;
      scenario.states[recoveryAt] = structuredClone(initial);
      return scenario;
    },
  },
  {
    id: 'timeline-lb-capacity', category: 'timeline', title: 'Timeline: shared LB capacity recovery',
    description: '32 clients with mixed access links keep sending while shared LB ingress and forwarding fall ' +
      'from 1 Gbit/s to 100 Mbit/s at 30 s, then recover to 1 Gbit/s at 90 s.',
    build(defaults) {
      const scenario = timelineExperiment(defaults);
      const initial = scenario.states['0'];
      const overload = structuredClone(initial);
      overload.resources.loadBalancer.ingress.bandwidthBitsPerSecond = 100_000_000;
      overload.resources.loadBalancer.forwardingBitsPerSecond = 100_000_000;
      scenario.states[overloadAt] = overload;
      scenario.states[recoveryAt] = structuredClone(initial);
      return scenario;
    },
  },
  {
    id: 'timeline-cpu-scaling', category: 'timeline', title: 'Timeline: CPU bottleneck and scale-up',
    description: '32 clients keep sending while LB CPU processing changes from zero cost to one core at 10 µs/packet ' +
      'at 30 s, then four cores at the same cost at 90 s. CPU waiting capacity stays at 128.',
    build(defaults) {
      const scenario = timelineExperiment(defaults);
      const initial = scenario.states['0'];
      initial.resources.loadBalancer.host = { cores: 1, packetProcessingTimeNanoseconds: 0, queueCapacity: 128 };
      const overload = structuredClone(initial);
      overload.resources.loadBalancer.host = { cores: 1, packetProcessingTimeNanoseconds: 10_000, queueCapacity: 128 };
      const recovery = structuredClone(overload);
      recovery.resources.loadBalancer.host!.cores = 4;
      scenario.states[overloadAt] = overload;
      scenario.states[recoveryAt] = recovery;
      return scenario;
    },
  },
];
