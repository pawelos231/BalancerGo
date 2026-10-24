import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment, presetClientGroup } from './base.ts';

// A simultaneous SYN burst makes admission drops visible without random loss.
export const queuePresets: readonly ScenarioPreset[] = [
  {
    id: 'tiny-queues',
    category: 'queues',
    title: 'Tiny LB input queues',
    description:
      '64 clients arrive together at a 1 Mbit/s LB receiver with 2 waiting slots per input queue. ' +
      'Full queues tail-drop arriving packets; endpoint queues keep 1024 slots.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.workload.clientGroups = [presetClientGroup('clients', 64)];
      scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond = 1_000_000;
      scenario.resources.loadBalancer.ingress.queueCapacity = 2;

      return scenario;
    },
  },
  {
    id: 'unbuffered-ingress',
    category: 'queues',
    title: 'Unbuffered LB input',
    description:
      '64 clients arrive together at a 1 Mbit/s LB receiver with no waiting slots. ' +
      'An incoming packet must start immediately or it is dropped.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.workload.clientGroups = [presetClientGroup('clients', 64)];
      scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond = 1_000_000;
      scenario.resources.loadBalancer.ingress.queueCapacity = 0;

      return scenario;
    },
  },
];
