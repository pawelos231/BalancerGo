import type { ExactInteger, Scenario, ScenarioHostConfig, ScenarioLinkConfig } from '../scenarios/types.ts';

export const LANGUAGE_VERSION = 1;

// These defaults belong to language version 1, independently of server CLI flags.
export function defaultLink(): ScenarioLinkConfig {
  return {
    bandwidthBitsPerSecond: 1_000_000_000,
    packetTravelTimeNanoseconds: 1_000_000,
    queueCapacity: 1024,
    lossRate: 0,
    queuePolicy: 'tailDrop',
    congestionThresholdPercent: 0,
    congestionSignalProbability: 1,
  };
}

export function defaultHost(): ScenarioHostConfig {
  return { cores: 1, packetProcessingTimeNanoseconds: 0, queueCapacity: 1024 };
}

// Every compilation gets independent nested objects; editing one result cannot affect another.
export function scenarioDefaults(duration: ExactInteger): Scenario {
  return {
    scenarioVersion: 2,
    fixed: {
      execution: {
        durationNanoseconds: duration,
        snapshotIntervalNanoseconds: 100_000_000,
        seed: 1,
        speed: 1,
        maxEvents: 5_000_000_000,
      },
      tcp: {
        mssBytes: 1024,
        receiveBufferBytes: 65_535,
        sackEnabled: false,
        ECNenabled: false,
        congestionControl: {
          mode: 'reno', fastRetransmit: true, cubicFastConvergence: true,
          initialWindowSegments: 4, maximumWindowSegments: 0,
          initialSlowStartThresholdBytes: 65_535,
        },
        rto: {
          mode: 'adaptive', exponentialBackoff: true,
          initialTimeoutNanoseconds: 1_000_000_000,
          minimumTimeoutNanoseconds: 200_000_000,
          maximumTimeoutNanoseconds: 60_000_000_000,
        },
        close: {
          mslNanoseconds: 30_000_000_000,
          finWaitTimeoutNanoseconds: 60_000_000_000,
          maxFINRetries: 5,
        },
        delayedAck: { enabled: false, segmentsPerACK: 2, maxDelayNanoseconds: 40_000_000 },
        clientIdleTimeoutNanoseconds: 20_000_000_000,
        backendHalfOpenIdleTimeoutNanoseconds: 5_000_000_000,
        backendEstablishedIdleTimeoutNanoseconds: 0,
      },
      loadBalancer: { id: 'lb' },
      network: { defaults: defaultLink(), profiles: [] },
      workload: { defaults: { packets: 10_000, bytes: -1, packetGapNanoseconds: 0 } },
    },
    states: {
      '0': {
        resources: {
          loadBalancer: {
            algorithm: 'roundRobin',
            ingress: { ...defaultLink(), packetTravelTimeNanoseconds: 0 },
            forwardingBitsPerSecond: 1_000_000_000,
            host: defaultHost(),
            mappingTimeouts: {
              halfOpenIdleTimeoutNanoseconds: 5_000_000_000,
              establishedIdleTimeoutNanoseconds: 50_000_000_000,
              closingIdleTimeoutNanoseconds: 120_000_000_000,
              resetIdleTimeoutNanoseconds: 10_000_000_000,
            },
          },
          backends: [],
        },
        workload: { clientGroups: [] },
      },
    },
  };
}
