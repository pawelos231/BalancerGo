import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment } from './base.ts';

// Loss belongs to a directed link; it does not mark a backend CPU as unhealthy.
export const lossPresets: readonly ScenarioPreset[] = [
  {
    id: 'lossy-access',
    category: 'loss',
    title: 'Lossy client uplinks',
    description:
      'Each client-to-LB packet has a seeded 7.5% loss probability after transmission. ' +
      'Reverse links and both backend paths have no random loss.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.workload.clientGroups[0].network.toLoadBalancer.lossRate = 0.075;

      return scenario;
    },
  },
  {
    id: 'blackholed-backend',
    category: 'loss',
    title: 'Blackholed backend link',
    description:
      'One LB-to-backend link loses every packet while the other backend remains reachable. ' +
      'This models a failed link, rather than a backend health or CPU failure.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.resources.backends[0].network.fromLoadBalancer.lossRate = 1;

      return scenario;
    },
  },
];
