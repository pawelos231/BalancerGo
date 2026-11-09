import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { formatVirtualSeconds, parseVirtualSeconds, projectPlannedTopology } from '../../../src/scenarios/plannedTopology.ts';
import { parseScenarioV2, validateScenarioV2 } from '../../../src/scenarios/v2.ts';

const fixture = readFileSync(new URL('../../../../examples/scenarios-v2/static.json', import.meta.url), 'utf8');

function scenario() {
  return parseScenarioV2(fixture);
}

test('planned time entry and formatting preserve exact nanoseconds without floating point', () => {
  const cases = [
    ['0', 0n],
    ['.000000001', 1n],
    [' 001.250000000 ', 1_250_000_000n],
    ['9007199.254740993', 9_007_199_254_740_993n],
    ['9223372036.854775807', 9_223_372_036_854_775_807n],
  ] as const;
  for (const [text, expected] of cases) {
    assert.equal(parseVirtualSeconds(text), expected);
    assert.equal(parseVirtualSeconds(formatVirtualSeconds(expected)), expected);
  }
  assert.equal(formatVirtualSeconds('1000000000'), '1');
  assert.equal(formatVirtualSeconds(1_020_000_000n), '1.02');
  for (const text of ['', ' ', '-1', '+1', '1e-9', '0.0000000001', 'NaN', 'Infinity', '1,5']) {
    assert.throws(() => parseVirtualSeconds(text), /at most 9 decimal places/);
  }
});

test('planned lookup uses the greatest exact state at or below arbitrary virtual time', () => {
  const input = scenario();
  const initial = input.states['0'];
  input.fixed.execution.durationNanoseconds = '9007199254740999';
  input.states = {
    '9007199254740993': structuredClone(initial),
    '10': structuredClone(initial),
    '0': initial,
    '9007199254740992': structuredClone(initial),
    '2': structuredClone(initial),
  };
  const checked = validateScenarioV2(input);
  const atZero = projectPlannedTopology(checked, 0n);
  assert.equal(atZero.stateAtNanoseconds, '0');
  assert.equal(atZero.previousAtNanoseconds, null);
  assert.equal(atZero.nextAtNanoseconds, '2');

  const between = projectPlannedTopology(checked, 9n);
  assert.equal(between.stateAtNanoseconds, '2');
  assert.equal(between.previousAtNanoseconds, '2');
  assert.equal(between.nextAtNanoseconds, '10');

  const adjacent = projectPlannedTopology(checked, 9_007_199_254_740_992n);
  assert.equal(adjacent.stateAtNanoseconds, '9007199254740992');
  assert.equal(adjacent.previousAtNanoseconds, '10');
  assert.equal(adjacent.nextAtNanoseconds, '9007199254740993');
  const next = projectPlannedTopology(checked, 9_007_199_254_740_993n);
  assert.equal(next.stateAtNanoseconds, '9007199254740993');
  assert.equal(next.previousAtNanoseconds, '9007199254740992');
  assert.equal(next.nextAtNanoseconds, null);

  const end = projectPlannedTopology(checked, BigInt(checked.fixed.execution.durationNanoseconds));
  assert.equal(end.stateAtNanoseconds, '9007199254740993');
  assert.equal(end.previousAtNanoseconds, '9007199254740993');
  assert.equal(end.atNanoseconds, '9007199254740999');
  assert.throws(() => projectPlannedTopology(checked, -1n), /between 0/);
  assert.throws(() => projectPlannedTopology(checked, 9_007_199_254_741_000n), /between 0/);
});

test('planned projection resolves directional profiles, explicit zero overrides and final stream bytes', () => {
  const input = scenario();
  input.fixed.workload.defaults.bytes = -1;
  input.fixed.network.profiles.push({
    id: 'slow',
    config: { bandwidthBitsPerSecond: 4_000_000, packetTravelTimeNanoseconds: 123, queueCapacity: 32, lossRate: 0.25 },
  });
  const group = input.states['0'].workload.clientGroups[0];
  group.network.toLoadBalancer = { bandwidthBitsPerSecond: 10_000_000, lossRate: 0.1 };
  group.network.fromLoadBalancer = { queueCapacity: 0 };
  group.stream = { packets: 7, packetGapNanoseconds: 50 };
  group.overrides = [
    {
      index: 0,
      network: { profile: 'slow', toLoadBalancer: { packetTravelTimeNanoseconds: 0, queueCapacity: 0, lossRate: 0 }, fromLoadBalancer: {} },
      stream: { packets: 3, packetGapNanoseconds: 0 },
    },
    { index: 1, network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: { packets: 0, bytes: 0 } },
  ];
  const checked = validateScenarioV2(input);
  const projected = projectPlannedTopology(checked, 0n).clientGroups[0];
  assert.equal(projected.network.toLoadBalancer.bandwidthBitsPerSecond, 10_000_000);
  assert.equal(projected.network.fromLoadBalancer.bandwidthBitsPerSecond, 20_000_000);
  assert.equal(projected.network.fromLoadBalancer.queueCapacity, 0);
  assert.equal(projected.stream.bytes, '7168');

  const overridden = projected.overrides[0];
  assert.equal(overridden.network.toLoadBalancer.bandwidthBitsPerSecond, 4_000_000);
  assert.equal(overridden.network.toLoadBalancer.packetTravelTimeNanoseconds, 0);
  assert.equal(overridden.network.toLoadBalancer.queueCapacity, 0);
  assert.equal(overridden.network.toLoadBalancer.lossRate, 0);
  assert.equal(overridden.network.fromLoadBalancer.packetTravelTimeNanoseconds, 123);
  assert.equal(overridden.network.fromLoadBalancer.queueCapacity, 32);
  assert.equal(overridden.network.fromLoadBalancer.lossRate, 0.25);
  assert.equal(overridden.stream.bytes, '3072');
  assert.equal(overridden.stream.packetGapNanoseconds, 0);
  assert.deepEqual(projected.overrides[1].network, projected.network);
  assert.equal(projected.overrides[1].stream.bytes, 0);
});

test('planned resource projection shows configured CPU, shared LB limits and physical/admin defaults', () => {
  const input = scenario();
  const state = input.states['0'];
  state.resources.loadBalancer.host = { cores: 3, packetProcessingTimeNanoseconds: 0, queueCapacity: 0 };
  state.resources.loadBalancer.forwardingBitsPerSecond = '9007199254740993';
  state.resources.loadBalancer.mappingTimeouts.halfOpenIdleTimeoutNanoseconds = 0;
  state.resources.backends[0].host = undefined;
  state.resources.backends[0].physicalAvailable = null;
  state.resources.backends[0].administrativeState = null;
  state.resources.backends[1].host = { cores: 2, packetProcessingTimeNanoseconds: 500, queueCapacity: 0 };
  state.resources.backends[1].physicalAvailable = false;
  state.resources.backends[1].administrativeState = 'drain';
  state.resources.backends[1].network.fromLoadBalancer = { lossRate: 0.4 };
  const projected = projectPlannedTopology(validateScenarioV2(input), 0n);
  assert.deepEqual(projected.loadBalancer.host, { cores: 3, packetProcessingTimeNanoseconds: 0, queueCapacity: 0 });
  assert.equal(projected.loadBalancer.forwardingBitsPerSecond, '9007199254740993');
  assert.equal(projected.loadBalancer.mappingTimeouts.halfOpenIdleTimeoutNanoseconds, 0);
  assert.equal(projected.loadBalancer.ingress.bandwidthBitsPerSecond, 100_000_000);
  assert.deepEqual(projected.backends[0].host, { cores: 1, packetProcessingTimeNanoseconds: 0, queueCapacity: 1024 });
  assert.equal(projected.backends[0].physicalAvailable, true);
  assert.equal(projected.backends[0].administrativeState, 'active');
  assert.deepEqual(projected.backends[1].host, { cores: 2, packetProcessingTimeNanoseconds: 500, queueCapacity: 0 });
  assert.equal(projected.backends[1].physicalAvailable, false);
  assert.equal(projected.backends[1].administrativeState, 'drain');
  assert.equal(projected.backends[1].network.fromLoadBalancer.lossRate, 0.4);
});

test('each planned state resolves against fixed defaults rather than the previous declaration', () => {
  const input = scenario();
  const initial = input.states['0'];
  initial.workload.clientGroups[0].stream = { bytes: 123 };
  initial.resources.backends[0].network.toLoadBalancer = { lossRate: 0.9 };
  initial.resources.backends[0].host = { cores: 8 };
  initial.resources.backends[0].physicalAvailable = false;
  const later = structuredClone(initial);
  later.workload.clientGroups[0].network = { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} };
  later.workload.clientGroups[0].stream = {};
  later.resources.backends[0].network.toLoadBalancer = {};
  later.resources.backends[0].host = undefined;
  later.resources.backends[0].physicalAvailable = undefined;
  input.states['1000'] = later;
  const projected = projectPlannedTopology(validateScenarioV2(input), 1000n);
  assert.equal(projected.clientGroups[0].network.toLoadBalancer.bandwidthBitsPerSecond, 100_000_000);
  assert.equal(projected.clientGroups[0].stream.bytes, 262144);
  assert.equal(projected.backends[0].network.toLoadBalancer.lossRate, 0);
  assert.equal(projected.backends[0].host.cores, 1);
  assert.equal(projected.backends[0].physicalAvailable, true);
});

test('planned declarations stay compact for 50000 slots and retain reserved overrides after a decrease', () => {
  const input = scenario();
  const initial = input.states['0'];
  initial.workload.clientGroups[0].count = 50_000;
  initial.workload.clientGroups[0].clientGapNanoseconds = 1;
  const smaller = structuredClone(initial);
  smaller.workload.clientGroups[0].count = 1;
  smaller.workload.clientGroups[0].overrides = [{
    index: 49_999,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: { bytes: 0 },
  }];
  input.states['1000'] = smaller;
  const removed = structuredClone(smaller);
  removed.workload.clientGroups = [];
  input.states['2000'] = removed;
  const checked = validateScenarioV2(input);
  const projected = projectPlannedTopology(checked, 0n);
  assert.equal(projected.clientGroups.length, 1);
  assert.equal(projected.clientGroups[0].count, 50_000);
  assert.equal(projected.clientGroups[0].overrides.length, 0);
  assert.ok(JSON.stringify(projected).length < 10_000);

  const decreased = projectPlannedTopology(checked, 1500n);
  assert.equal(decreased.clientGroups[0].count, 1);
  assert.equal(decreased.clientGroups[0].overrides.length, 1);
  assert.equal(decreased.clientGroups[0].overrides[0].index, 49_999);
  assert.equal(decreased.clientGroups[0].overrides[0].reserved, true);
  assert.equal(decreased.clientGroups[0].overrides[0].stream.bytes, 0);
  assert.equal(projectPlannedTopology(checked, 2000n).clientGroups.length, 0);
});

test('planned projection does not mutate or retain mutable source settings', () => {
  const input = scenario();
  input.states['0'].workload.clientGroups[0].overrides = [{
    index: 0,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: {},
  }];
  const before = parseScenarioV2(JSON.stringify(input));
  const projected = projectPlannedTopology(input, 0n);
  assert.deepEqual(input, before);
  projected.loadBalancer.host.cores = 10;
  projected.loadBalancer.ingress.queueCapacity = 0;
  projected.loadBalancer.mappingTimeouts.halfOpenIdleTimeoutNanoseconds = 0;
  projected.backends[0].network.toLoadBalancer.lossRate = 0.9;
  projected.backends[0].host.cores = 10;
  projected.clientGroups[0].network.fromLoadBalancer.queueCapacity = 0;
  projected.clientGroups[0].stream.bytes = 0;
  projected.clientGroups[0].overrides[0].network.toLoadBalancer.queueCapacity = 0;
  projected.clientGroups[0].overrides[0].stream.bytes = 0;
  assert.deepEqual(input, before);
});
