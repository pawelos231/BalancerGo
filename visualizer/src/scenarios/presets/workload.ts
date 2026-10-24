import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment, presetClientGroup } from './base.ts';

// Arrival times and byte boundaries exercise workload behavior independently of rates.
export const workloadPresets: readonly ScenarioPreset[] = [
  {
    id: 'staggered-arrivals',
    category: 'workload',
    title: 'Staggered client waves',
    description:
      'Four groups of 4 clients start at 0, 5, 20 and 200 virtual seconds, with 250 ms between clients. ' +
      'The last group starts beyond the 180-second horizon.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      const waves = [
        { id: 'wave-0s', startAt: 0 },
        { id: 'wave-5s', startAt: 5_000_000_000 },
        { id: 'wave-20s', startAt: 20_000_000_000 },
        { id: 'after-horizon', startAt: 200_000_000_000 },
      ];

      scenario.workload.clientGroups = waves.map(({ id, startAt }) => {
        const group = presetClientGroup(id, 4);
        group.startAtNanoseconds = startAt;
        group.clientGapNanoseconds = 250_000_000;

        return group;
      });

      return scenario;
    },
  },
  {
    id: 'mixed-streams',
    category: 'workload',
    title: 'Short and long streams',
    description:
      '8 short streams include explicit 0, 1, 1024 and 1025-byte cases; 8 long streams send 1 GiB each. ' +
      'These boundaries show empty payloads and partial final segments.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      const shortStreams = presetClientGroup('short-streams', 8);
      const longStreams = presetClientGroup('long-streams', 8);

      shortStreams.stream.bytes = 4096;
      shortStreams.overrides = [0, 1, 1024, 1025].map((bytes, index) => ({
        index,
        network: { toLoadBalancer: {}, fromLoadBalancer: {} },
        stream: { bytes },
      }));
      scenario.workload.clientGroups = [shortStreams, longStreams];

      return scenario;
    },
  },
];
