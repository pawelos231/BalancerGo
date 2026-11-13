import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { resolveNetwork } from '../../../src/scenarios/settings.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';
import { parseScenario } from '../../../src/scenarios/validation.ts';

const names = ['burst', 'backend-failure', 'slow-backend', 'cpu-bottleneck', 'asymmetric-links'];
const fixture = (name: string) => readFileSync(
  new URL(`../../../../examples/language/${name}`, import.meta.url),
  'utf8',
);
const recipe = (name: string) => compileScenario(fixture(`${name}.lb`));

function backendLinks(scenario: Scenario, index = 0) {
  const defaults = scenario.fixed.network.defaults;
  const profiles = new Map(scenario.fixed.network.profiles.map(profile => [profile.id, profile.config]));
  return resolveNetwork(
    { toLoadBalancer: defaults, fromLoadBalancer: defaults },
    scenario.states['0'].resources.backends[index].network,
    profiles,
    'recipe.backend',
  );
}

// Each checked output remains ready for the unchanged Go JSON runner.
test('language recipes compile to matching native fixtures within short unpaced runs', () => {
  for (const name of names) {
    const scenario = recipe(name);
    assert.deepEqual(scenario, parseScenario(fixture(`${name}.json`)), name);
    const duration = BigInt(scenario.fixed.execution.durationNanoseconds);
    assert.ok(duration > 0n && duration < 10_000_000_000n, name);
    assert.equal(scenario.fixed.execution.speed, 0, name);
    for (const timestamp of Object.keys(scenario.states)) assert.ok(BigInt(timestamp) < duration, name);
  }
});

// The original budget extends past the stop; count zero retains the group and its settings.
test('burst recipe stops a still-active birth schedule without removing resources', () => {
  const scenario = recipe('burst');
  assert.deepEqual(Object.keys(scenario.states), ['0', '2000000000', '4000000000']);
  assert.equal(scenario.states['0'].workload.clientGroups.some(group => group.id === 'burst'), false);
  const burst = scenario.states['2000000000'].workload.clientGroups.find(group => group.id === 'burst');
  const stopped = scenario.states['4000000000'].workload.clientGroups.find(group => group.id === 'burst');
  assert.ok(burst);
  assert.equal(burst.count, 1000);
  assert.equal(burst.startAtNanoseconds, 2_000_000_000);
  assert.equal(burst.clientGapNanoseconds, 10_000_000);
  assert.equal(burst.stream.bytes, 20_000);
  const lastBirth = BigInt(burst.startAtNanoseconds)
    + (BigInt(burst.count) - 1n) * BigInt(burst.clientGapNanoseconds);
  assert.ok(lastBirth > 4_000_000_000n);
  assert.deepEqual(stopped, { ...burst, count: 0 });
  assert.deepEqual(scenario.states['4000000000'].resources, scenario.states['0'].resources);
});

// Arrival times straddle both state changes while only one backend's physical flag changes.
test('failure recipe preserves identities and clients across physical outage and recovery', () => {
  const scenario = recipe('backend-failure');
  const initial = scenario.states['0'];
  const failed = scenario.states['3000000000'];
  const restored = scenario.states['5000000000'];
  assert.equal(scenario.fixed.tcp.congestionControl?.mode, 'newReno');
  assert.equal(scenario.fixed.tcp.congestionControl?.fastRetransmit, true);
  assert.equal(scenario.fixed.tcp.sackEnabled, true);
  assert.deepEqual(initial.resources.backends.map(backend => backend.id), ['api-0', 'api-1']);
  assert.deepEqual(failed.resources.backends[0], { ...initial.resources.backends[0], physicalAvailable: false });
  assert.deepEqual(restored.resources.backends[0], { ...initial.resources.backends[0], physicalAvailable: true });
  assert.deepEqual(failed.resources.backends[1], initial.resources.backends[1]);
  assert.deepEqual(restored.workload, initial.workload);
  const group = initial.workload.clientGroups[0];
  const lastBirth = BigInt(group.startAtNanoseconds)
    + (BigInt(group.count) - 1n) * BigInt(group.clientGapNanoseconds);
  assert.ok(BigInt(group.startAtNanoseconds) < 3_000_000_000n && lastBirth > 5_000_000_000n);
  assert.equal(group.clientGapNanoseconds, 100_000_000);
});

test('slow-backend recipe gives round robin two distinct symmetric backend rates', () => {
  const scenario = recipe('slow-backend');
  const slow = backendLinks(scenario, 0);
  const fast = backendLinks(scenario, 1);
  assert.equal(scenario.states['0'].resources.loadBalancer.algorithm, 'roundRobin');
  assert.equal(slow.toLoadBalancer.bandwidthBitsPerSecond, 8_000_000);
  assert.equal(slow.fromLoadBalancer.bandwidthBitsPerSecond, 8_000_000);
  assert.equal(fast.toLoadBalancer.bandwidthBitsPerSecond, 800_000_000);
  assert.equal(fast.fromLoadBalancer.bandwidthBitsPerSecond, 800_000_000);
});

// Host queues are independent of link queues, and processing costs exceed link serialization.
test('CPU recipe keeps host service costs and waiting queues separate from network capacity', () => {
  const scenario = recipe('cpu-bottleneck');
  const resources = scenario.states['0'].resources;
  assert.deepEqual(resources.loadBalancer.host, {
    cores: 1,
    packetProcessingTimeNanoseconds: 10_000,
    queueCapacity: 32,
  });
  assert.deepEqual(resources.backends[0].host, {
    cores: 1,
    packetProcessingTimeNanoseconds: 50_000,
    queueCapacity: 32,
  });
  assert.equal(resources.loadBalancer.ingress.queueCapacity, 128);
  const links = backendLinks(scenario);
  assert.equal(links.fromLoadBalancer.queueCapacity, 128);
  const packetBits = (BigInt(scenario.fixed.tcp.mssBytes!) + 40n) * 8n;
  const serialization = packetBits * 1_000_000_000n / BigInt(links.fromLoadBalancer.bandwidthBitsPerSecond);
  assert.ok(BigInt(resources.backends[0].host!.packetProcessingTimeNanoseconds!) > 10n * serialization);
});

test('asymmetric recipe slows the backend ACK return direction independently of incoming data', () => {
  const links = backendLinks(recipe('asymmetric-links'));
  assert.equal(links.toLoadBalancer.bandwidthBitsPerSecond, 8_000_000);
  assert.equal(links.toLoadBalancer.packetTravelTimeNanoseconds, 5_000_000);
  assert.equal(links.fromLoadBalancer.bandwidthBitsPerSecond, 800_000_000);
  assert.equal(links.fromLoadBalancer.packetTravelTimeNanoseconds, 1_000_000);
});
