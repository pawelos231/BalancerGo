import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment, presetBackends, presetClientGroup } from './base.ts';

// Routing and empty workloads expose boundary behavior without introducing link loss.
export const routingPresets: readonly ScenarioPreset[] = [
  {
    id: 'seeded-random',
    category: 'routing',
    title: 'Seeded random routing',
    description:
      '16 clients use random routing across 4 identical backends with the exact seed 9007199254740993. ' +
      'The seed stays a decimal string so every digit survives JSON exchange.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.execution.seed = '9007199254740993';
      scenario.resources.loadBalancer.algorithm = 'random';
      scenario.resources.backends = presetBackends(4);

      return scenario;
    },
  },
  {
    id: 'no-backends',
    category: 'edge-cases',
    title: 'No available backends',
    description:
      '8 clients attempt to connect to an LB with no backends. ' +
      'Their SYN attempts exercise the retry and connection cleanup path.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.resources.backends = [];
      scenario.workload.clientGroups = [presetClientGroup('clients', 8)];

      return scenario;
    },
  },
  {
    id: 'idle-network',
    category: 'edge-cases',
    title: 'Idle network',
    description:
      'Two backends and all shared limits remain present, but no clients are created. ' +
      'This separates an idle topology from traffic and queue activity.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.workload.clientGroups = [presetClientGroup('clients', 0)];

      return scenario;
    },
  },
];
