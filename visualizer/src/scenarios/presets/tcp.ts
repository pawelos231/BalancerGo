import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import { isolatedExperiment } from './base.ts';

// A one-segment cwnd cannot release a partner until its first segment is ACKed.
export const tcpPresets: readonly ScenarioPreset[] = [
  {
    id: 'delayed-ack-small-window',
    category: 'transport',
    title: 'Delayed ACK with a one-segment window',
    description: '16 Reno clients have cwnd capped at one segment and a 40 ms delayed-ACK timer. ' +
      'A second segment cannot follow before the ACK. Disable delayed ACK to compare transfer rates.',
    build(defaults) {
      const scenario = isolatedExperiment(defaults);
      scenario.tcp = {
        ...scenario.tcp,
        receiveBufferBytes: 65535,
        sackEnabled: false,
        ECNenabled: false,
        delayedAck: {
          enabled: true,
          segmentsPerACK: 2,
          maxDelayNanoseconds: 40_000_000,
        },
        congestionControl: {
          mode: 'reno',
          fastRetransmit: true,
          initialWindowSegments: 1,
          maximumWindowSegments: 1,
          initialSlowStartThresholdBytes: 65535,
        },
      };
      return scenario;
    },
  },
];
