import assert from 'node:assert/strict';
import test from 'node:test';
import { scenarioPresets } from '../src/scenarios/presets.ts';
import { scenarioFixture } from './scenarioFixture.ts';

test('preset factories keep backend defaults exact and return independent documents', () => {
  const defaults = scenarioFixture();
  const original = structuredClone(defaults);
  assert.equal(scenarioPresets.length, 4);
  assert.equal(new Set(scenarioPresets.map((preset) => preset.id)).size, 4);

  for (const preset of scenarioPresets) {
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
