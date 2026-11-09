import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment } from './base.ts';

// Only the LB-to-backend data links signal congestion; ACK return links stay fast.
export const ecnPresets: readonly ScenarioPreset[] = [
  {
    id: 'ecn-congestion',
    category: 'queues',
    title: 'ECN at a forward bottleneck',
    description:
      '16 Reno clients negotiate ECN. Two 10 Mbit/s LB-to-backend links mark data at 25% ' +
      'waiting occupancy and above, with 64 slots each. ACK return paths stay at 1 Gbit/s with no random loss.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.fixed.tcp = {
        ...scenario.fixed.tcp,
        ECNenabled: true,
        congestionControl: {
          ...scenario.fixed.tcp?.congestionControl,
          mode: 'reno',
          fastRetransmit: true,
          initialWindowSegments: 4,
          initialSlowStartThresholdBytes: 65535,
        },
      };
      for (const backend of scenario.states['0'].resources.backends) {
        backend.network.fromLoadBalancer = {
          bandwidthBitsPerSecond: 10_000_000,
          queueCapacity: 64,
          queuePolicy: 'threshold',
          congestionThresholdPercent: 25,
          congestionSignalProbability: 1,
        };
      }
      return scenario;
    },
  },
];
