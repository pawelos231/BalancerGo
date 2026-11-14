import type { Scenario, ScenarioClientGroup } from '../src/scenarios/types.ts';
import type { ExperimentContext } from '../src/experiment.ts';

// Small public DTO fixtures for tests that edit resources independently of the catalog.
export function fixtureBackends(count: number): Scenario['states'][string]['resources']['backends'] {
  return Array.from({ length: count }, (_, index) => ({
    id: `backend-${index + 1}`, name: `Backend ${index + 1}`,
    address: `10.0.0.${index + 1}`, port: 80,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  }));
}

export function fixtureClientGroup(id: string, count: number): ScenarioClientGroup {
  return {
    id, count, startAtNanoseconds: 0, clientGapNanoseconds: 0,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: {},
  };
}

export function experimentFixture(): ExperimentContext {
  return {
    model: 'test-model-v1',
    scenarioSha256: '0123456789abcdef'.repeat(4),
    execution: { headless: false, speed: 0, traceEnabled: false },
    build: {
      goVersion: 'go1.25.0', moduleVersion: '', goos: 'windows', goarch: 'amd64',
      vcsRevision: 'test-revision', vcsModified: false,
    },
  };
}

// Keep nontrivial defaults so preset tests notice accidentally reset inherited settings.
export function scenarioFixture(): Scenario {
  return {
    scenarioVersion: 2,
    fixed: {
      execution: {
        durationNanoseconds: '9223372036854775807',
        snapshotIntervalNanoseconds: 12_345_678,
        seed: '-9007199254740993', speed: 0,
        maxEvents: '18446744073709551615',
      },
      tcp: {},
      loadBalancer: { id: 'native-lb' },
      network: {
        defaults: { bandwidthBitsPerSecond: 100_000_000, packetTravelTimeNanoseconds: 765_432, queueCapacity: 17, lossRate: 0.01 },
        profiles: [{ id: 'old-profile', config: { queueCapacity: 2 } }],
      },
      workload: { defaults: { packets: 99, bytes: -1, packetGapNanoseconds: 456 } },
    },
    states: {
      '0': {
        resources: {
          loadBalancer: {
            algorithm: 'random', forwardingBitsPerSecond: 100_000_000,
            ingress: { bandwidthBitsPerSecond: 100_000_000, packetTravelTimeNanoseconds: 123, queueCapacity: 5, lossRate: 0.02 },
            mappingTimeouts: {},
          },
          backends: [],
        },
        workload: { clientGroups: [] },
      },
    },
  };
}
