import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment, presetBackends, presetClientGroup } from './base.ts';

// These rates reach the 1 ns serialization floor; client count is configured demand.
export const performancePresets: readonly ScenarioPreset[] = [
  {
    id: 'simulator-limits',
    category: 'performance',
    title: 'Absolute simulator limits',
    description:
      '50,000 configured clients at 10 TB/s and 100 backends at 100 TB/s share 10 PB/s LB input and output limits. ' +
      'Runs unpaced for 1 virtual ms.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);

      scenario.execution = {
        durationNanoseconds: 1_000_000,
        snapshotIntervalNanoseconds: 1_000_000,
        seed: 1,
        speed: 0,
        maxEvents: 20_000_000,
      };
      scenario.network.defaults = {
        bandwidthBitsPerSecond: 80_000_000_000_000,
        packetTravelTimeNanoseconds: 0,
        queueCapacity: 4096,
        lossRate: 0,
      };
      scenario.network.profiles = [
        {
          id: 'limits-backend',
          config: { bandwidthBitsPerSecond: 800_000_000_000_000 },
        },
      ];
      scenario.resources.loadBalancer.ingress = {
        bandwidthBitsPerSecond: '80000000000000000',
        packetTravelTimeNanoseconds: 0,
        queueCapacity: 131_072,
        lossRate: 0,
      };
      scenario.resources.loadBalancer.forwardingBitsPerSecond = '80000000000000000';
      scenario.resources.backends = presetBackends(100);

      for (const backend of scenario.resources.backends) {
        backend.network.profile = 'limits-backend';
      }

      scenario.workload.clientGroups = [presetClientGroup('clients', 50_000)];

      return scenario;
    },
  },
];
