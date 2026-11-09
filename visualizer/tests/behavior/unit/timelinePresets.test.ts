import assert from 'node:assert/strict';
import test from 'node:test';
import { projectPlannedTopology } from '../../../src/scenarios/plannedTopology.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

function timelinePreset(id: string) {
  const preset = scenarioPresets.find(entry => entry.id === id);
  assert.ok(preset, `missing preset ${id}`);
  return validateScenario(preset.build(scenarioFixture()));
}

test('timeline presets pin TCP and contain complete independent states through recovery', () => {
  const presets = scenarioPresets.filter(preset => preset.category === 'timeline');
  assert.equal(presets.length, 3);

  for (const preset of presets) {
    const scenario = timelinePreset(preset.id);
    assert.deepEqual(Object.keys(scenario.states), ['0', '30000000000', '90000000000'], preset.id);
    assert.equal(scenario.fixed.execution.durationNanoseconds, 180_000_000_000);
    assert.equal(scenario.fixed.execution.maxEvents, 5_000_000_000);
    assert.equal(scenario.fixed.execution.seed, 1);
    assert.equal(scenario.fixed.tcp.mssBytes, 1460);
    assert.equal(scenario.fixed.tcp.sackEnabled, true);
    assert.equal(scenario.fixed.tcp.congestionControl?.mode, 'newReno');
    assert.equal(scenario.fixed.tcp.clientIdleTimeoutNanoseconds, 0);
    assert.equal(scenario.fixed.tcp.backendEstablishedIdleTimeoutNanoseconds, 0);

    const initial = projectPlannedTopology(scenario, 0n);
    const overload = projectPlannedTopology(scenario, 30_000_000_000n);
    const recovery = projectPlannedTopology(scenario, 90_000_000_000n);
    assert.equal(projectPlannedTopology(scenario, 29_999_999_999n).stateAtNanoseconds, '0');
    assert.equal(overload.stateAtNanoseconds, '30000000000');
    assert.equal(recovery.stateAtNanoseconds, '90000000000');
    assert.deepEqual(overload.backends, initial.backends);
    assert.deepEqual(recovery.backends, initial.backends);
    assert.deepEqual(initial.backends.map(backend => backend.network.fromLoadBalancer.bandwidthBitsPerSecond), [1_000_000_000, 2_000_000_000]);

    // Editing one full state must not edit either neighboring declaration.
    scenario.states['30000000000'].resources.loadBalancer.ingress.queueCapacity = 7;
    assert.equal(scenario.states['0'].resources.loadBalancer.ingress.queueCapacity, 4096);
    assert.equal(scenario.states['90000000000'].resources.loadBalancer.ingress.queueCapacity, 4096);
  }
});

test('arrival preview shows lifetime budgets and stops generators at 90 seconds', () => {
  const scenario = timelinePreset('timeline-arrival-burst');
  const initial = projectPlannedTopology(scenario, 0n);
  const overload = projectPlannedTopology(scenario, 60_000_000_000n);
  const recovery = projectPlannedTopology(scenario, 90_000_000_000n);

  assert.deepEqual(initial.clientGroups.map(group => [group.id, group.count]), [['baseline', 8]]);
  assert.deepEqual(overload.clientGroups.map(group => [group.id, group.count]), [
    ['baseline', 8], ['burst', 24], ['arrivals', 120],
  ]);
  assert.equal(overload.clientGroups[1].startAtNanoseconds, 30_000_000_000);
  assert.equal(overload.clientGroups[1].clientGapNanoseconds, 0);
  assert.equal(overload.clientGroups[2].clientGapNanoseconds, 1_000_000_000);
  assert.deepEqual(recovery.clientGroups, initial.clientGroups);
  assert.equal(initial.clientGroups[0].network.toLoadBalancer.bandwidthBitsPerSecond, 20_000_000);
  assert.equal(overload.clientGroups[1].network.toLoadBalancer.bandwidthBitsPerSecond, 2_000_000_000);
  const baselinePayloadBits = BigInt(initial.clientGroups[0].stream.bytes) * 8n;
  const baselineRate = BigInt(initial.clientGroups[0].network.toLoadBalancer.bandwidthBitsPerSecond);
  assert.ok(baselinePayloadBits > 90n * baselineRate, 'baseline streams must outlast the stop-arrivals transition');
  // This projection describes generators; runtime retains clients already born.
});

test('capacity and CPU previews show both the bottleneck and the later recovery', () => {
  const capacity = timelinePreset('timeline-lb-capacity');
  const rates = [0n, 30_000_000_000n, 90_000_000_000n].map(at => {
    const lb = projectPlannedTopology(capacity, at).loadBalancer;
    return [lb.ingress.bandwidthBitsPerSecond, lb.forwardingBitsPerSecond];
  });
  assert.deepEqual(rates, [
    [1_000_000_000, 1_000_000_000], [100_000_000, 100_000_000], [1_000_000_000, 1_000_000_000],
  ]);

  const cpu = timelinePreset('timeline-cpu-scaling');
  const hosts = [0n, 30_000_000_000n, 90_000_000_000n].map(at => projectPlannedTopology(cpu, at).loadBalancer.host);
  assert.deepEqual(hosts, [
    { cores: 1, packetProcessingTimeNanoseconds: 0, queueCapacity: 128 },
    { cores: 1, packetProcessingTimeNanoseconds: 10_000, queueCapacity: 128 },
    { cores: 4, packetProcessingTimeNanoseconds: 10_000, queueCapacity: 128 },
  ]);
  for (const scenario of [capacity, cpu]) {
    const initial = scenario.states['0'];
    assert.deepEqual(scenario.states['30000000000'].workload, initial.workload);
    assert.deepEqual(scenario.states['90000000000'].workload, initial.workload);
    assert.deepEqual(initial.workload.clientGroups.map(group => group.count), [16, 16]);
    assert.equal(scenario.fixed.workload.defaults.bytes, 1_073_741_824);

    const payloadBits = initial.workload.clientGroups.reduce((total, group) => (
      total + BigInt(group.count) * BigInt(scenario.fixed.workload.defaults.bytes) * 8n
    ), 0n);
    const maximumSharedRate = BigInt(initial.resources.loadBalancer.forwardingBitsPerSecond);
    assert.ok(payloadBits > 90n * maximumSharedRate, 'payload must outlast recovery even at the initial LB rate');
  }
});
