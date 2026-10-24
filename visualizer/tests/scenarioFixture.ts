import type { Scenario } from '../src/scenarios/types.ts';

// Keep nontrivial defaults so preset tests notice accidentally reset inherited settings.
export function scenarioFixture(): Scenario {
  return {
    version: 1,
    execution: {
      durationNanoseconds: '9223372036854775807',
      snapshotIntervalNanoseconds: 12_345_678,
      seed: '-9007199254740993',
      speed: 0,
      maxEvents: '18446744073709551615',
    },
    network: {
      defaults: {
        bandwidthBitsPerSecond: 100_000_000,
        packetTravelTimeNanoseconds: 765_432,
        queueCapacity: 17,
        lossRate: 0.01,
      },
      profiles: [{ id: 'old-profile', config: { queueCapacity: 2 } }],
    },
    resources: {
      loadBalancer: {
        id: 'native-lb',
        algorithm: 'random',
        forwardingBitsPerSecond: 100_000_000,
        ingress: {
          bandwidthBitsPerSecond: 100_000_000,
          packetTravelTimeNanoseconds: 123,
          queueCapacity: 5,
          lossRate: 0.02,
        },
      },
      backends: [],
    },
    workload: {
      defaults: { packets: 99, bytes: -1, packetGapNanoseconds: 456 },
      clientGroups: [],
    },
  };
}
