import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment, presetClientGroup } from './base.ts';

// Directional patches separate data-path limits from the return path.
export const linkPresets: readonly ScenarioPreset[] = [
  {
    id: 'asymmetric-clients',
    category: 'links',
    title: 'Asymmetric client links',
    description:
      'Two groups of 8 clients: one has a 10 Mbit/s uplink, the other a 10 Mbit/s downlink. ' +
      'The opposite direction stays at 1 Gbit/s.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      const uploadLimited = presetClientGroup('upload-limited', 8);
      const downloadLimited = presetClientGroup('download-limited', 8);

      uploadLimited.network.toLoadBalancer.bandwidthBitsPerSecond = 10_000_000;
      downloadLimited.network.fromLoadBalancer.bandwidthBitsPerSecond = 10_000_000;
      scenario.workload.clientGroups = [uploadLimited, downloadLimited];

      return scenario;
    },
  },
  {
    id: 'mixed-latency',
    category: 'links',
    title: 'Near and distant clients',
    description:
      'Two groups of 8 clients have 0.1 ms and 25 ms packet travel time in each direction. ' +
      'All configured link rates stay at 1 Gbit/s.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      const nearClients = presetClientGroup('near-clients', 8);
      const farClients = presetClientGroup('far-clients', 8);

      scenario.network.profiles = [
        {
          id: 'near-client',
          config: { packetTravelTimeNanoseconds: 100_000 },
        },
        {
          id: 'far-client',
          config: { packetTravelTimeNanoseconds: 25_000_000 },
        },
      ];
      nearClients.network.profile = 'near-client';
      farClients.network.profile = 'far-client';
      scenario.workload.clientGroups = [nearClients, farClients];

      return scenario;
    },
  },
  {
    id: 'slow-ack-path',
    category: 'links',
    title: 'Slow backend return path',
    description:
      'Both backend-to-LB links run at 64 kbit/s, delaying ACKs and other return packets. ' +
      'Client links and the LB-to-backend direction stay at 1 Gbit/s.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);

      for (const backend of scenario.resources.backends) {
        backend.network.toLoadBalancer.bandwidthBitsPerSecond = 64_000;
      }

      return scenario;
    },
  },
];
