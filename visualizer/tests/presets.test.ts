import assert from 'node:assert/strict';
import test from 'node:test';
import { scenarioFormValues, validateScenarioField } from '../src/scenarios/draft.ts';
import type { ScenarioField } from '../src/scenarios/draft.ts';
import { scenarioPresets } from '../src/scenarios/presets.ts';
import { scenarioFixture } from './scenarioFixture.ts';

test('preset factories keep backend defaults exact and return independent documents', () => {
  const defaults = scenarioFixture();
  const original = structuredClone(defaults);
  assert.equal(new Set(scenarioPresets.map((preset) => preset.id)).size, scenarioPresets.length);
  const networkPresets = scenarioPresets.filter((preset) => preset.id !== 'simulator-throughput');
  assert.equal(networkPresets.length, 4);

  for (const preset of networkPresets) {
    const result = preset.build(defaults);
    assert.equal(result.execution.seed, '-9007199254740993');
    assert.equal(result.execution.maxEvents, '18446744073709551615');
    assert.equal(result.execution.snapshotIntervalNanoseconds, 12_345_678);
    assert.equal(result.execution.durationNanoseconds, 180_000_000_000);
    assert.equal(result.execution.speed, 1);
    assert.equal(result.network.defaults.packetTravelTimeNanoseconds, 765_432);
    assert.equal(result.network.defaults.queueCapacity, 17);
    assert.equal(result.resources.loadBalancer.ingress.queueCapacity, 5);
    assert.equal(result.resources.loadBalancer.ingress.packetTravelTimeNanoseconds, 123);
    assert.equal(result.resources.loadBalancer.algorithm, 'random');
    assert.equal(result.workload.defaults.bytes, 1_073_741_824);
    assert.equal(result.workload.defaults.packetGapNanoseconds, 0);
    assert.equal(result.resources.backends.length, 2);
    assert.equal(new Set(result.resources.backends.map((backend) => backend.address)).size, 2);

    result.resources.loadBalancer.ingress.queueCapacity = 999;
    assert.deepEqual(defaults, original);
    assert.equal(preset.build(defaults).resources.loadBalancer.ingress.queueCapacity, 5);
  }
});

test('preset profiles and shared limits identify four different experiments', () => {
  const defaults = scenarioFixture();
  const mixed = scenarioPresets.find((preset) => preset.id === 'mixed-clients')!.build(defaults);
  assert.deepEqual(mixed.workload.clientGroups.map((group) => [group.id, group.count]), [
    ['slow-clients', 8], ['fast-clients', 8],
  ]);
  assert.equal(mixed.workload.clientGroups[0].network.profile, 'slow-client');
  assert.equal(mixed.workload.clientGroups[1].network.profile, undefined);
  assert.deepEqual(mixed.network.profiles, [{
    id: 'slow-client', config: { bandwidthBitsPerSecond: 10_000_000 },
  }]);

  const backends = scenarioPresets.find((preset) => preset.id === 'mixed-backends')!.build(defaults);
  assert.equal(backends.resources.backends[0].network.profile, 'slow-backend');
  assert.equal(backends.resources.backends[1].network.profile, undefined);

  const ingress = scenarioPresets.find((preset) => preset.id === 'ingress-bottleneck')!.build(defaults);
  const egress = scenarioPresets.find((preset) => preset.id === 'egress-bottleneck')!.build(defaults);
  for (const result of [ingress, egress]) {
    assert.equal(result.workload.clientGroups[0].count, 32);
    assert.equal(result.network.defaults.bandwidthBitsPerSecond, 1_000_000_000);
  }
  assert.equal(ingress.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 10_000_000);
  assert.equal(ingress.resources.loadBalancer.forwardingBitsPerSecond, 1_000_000_000);
  assert.equal(egress.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 1_000_000_000);
  assert.equal(egress.resources.loadBalancer.forwardingBitsPerSecond, 10_000_000);
});

test('throughput preset keeps demand bounded, reproducible and independent of network defaults', () => {
  const defaults = scenarioFixture();
  const before = structuredClone(defaults);
  const preset = scenarioPresets.find((entry) => entry.id === 'simulator-throughput')!;
  assert.equal(preset.title, 'Simulator throughput');
  const scenario = preset.build(defaults);
  assert.equal(scenario.execution.durationNanoseconds, 5_000_000);
  assert.equal(scenario.execution.snapshotIntervalNanoseconds, 50_000);
  assert.equal(scenario.execution.speed, 0);
  assert.equal(scenario.execution.seed, 1);
  assert.equal(scenario.execution.maxEvents, 50_000_000);
  assert.equal(scenario.resources.loadBalancer.algorithm, 'roundRobin');
  assert.deepEqual(scenario.workload.clientGroups.map((group) => [
    group.count, group.startAtNanoseconds, group.clientGapNanoseconds,
  ]), [[50, 0, 0]]);
  assert.equal(scenario.workload.defaults.bytes, 1_073_741_824);
  assert.equal(scenario.workload.defaults.packetGapNanoseconds, 0);

  const backendProfile = scenario.network.profiles!.find((profile) => profile.id === 'throughput-backend')!;
  assert.equal(scenario.resources.backends.length, 5);
  for (const backend of scenario.resources.backends) {
    assert.equal(backend.network.profile, backendProfile.id);
    assert.deepEqual(backend.network.toLoadBalancer, {});
    assert.deepEqual(backend.network.fromLoadBalancer, {});
  }
  assert.equal(new Set(scenario.resources.backends.map((backend) => backend.id)).size, 5);
  assert.equal(new Set(scenario.resources.backends.map((backend) => `${backend.address}:${backend.port}`)).size, 5);

  // Both directions inherit zero travel/loss and finite waiting slots.
  assert.equal(scenario.network.defaults.packetTravelTimeNanoseconds, 0);
  assert.equal(scenario.network.defaults.lossRate, 0);
  assert.equal(scenario.network.defaults.queueCapacity, 4096);
  assert.equal(scenario.resources.loadBalancer.ingress.packetTravelTimeNanoseconds, 0);
  assert.equal(scenario.resources.loadBalancer.ingress.lossRate, 0);
  assert.equal(scenario.resources.loadBalancer.ingress.queueCapacity, 4096);

  const rates = [
    scenario.network.defaults.bandwidthBitsPerSecond,
    backendProfile.config.bandwidthBitsPerSecond!,
    scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond,
    scenario.resources.loadBalancer.forwardingBitsPerSecond,
  ];
  assert.deepEqual(rates, [
    80_000_000_000_000,
    800_000_000_000_000,
    8_000_000_000_000_000,
    8_000_000_000_000_000,
  ]);
  for (const rate of rates) {
    assert.ok(Number.isSafeInteger(rate));
    // Every ACK and full segment reaches the numerical floor, rather than a link limit.
    for (const bytes of [40n, 1064n]) {
      const bitsNanoseconds = bytes * 8n * 1_000_000_000n;
      const nanoseconds = (bitsNanoseconds + BigInt(rate) - 1n) / BigInt(rate);
      assert.equal(nanoseconds, 1n);
    }
  }

  const fields = scenarioFormValues(scenario);
  for (const [name, raw] of Object.entries(fields)) {
    assert.equal(validateScenarioField(name as ScenarioField, raw), undefined, name);
  }
  assert.deepEqual(defaults, before);
  assert.deepEqual(preset.build(defaults), scenario);
  scenario.resources.backends[0].network.fromLoadBalancer.lossRate = 1;
  assert.deepEqual(preset.build(defaults).resources.backends[0].network.fromLoadBalancer, {});
});
