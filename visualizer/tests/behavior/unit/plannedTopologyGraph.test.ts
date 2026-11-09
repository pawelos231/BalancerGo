import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { projectPlannedTopology } from '../../../src/scenarios/plannedTopology.ts';
import { projectPlannedTopologyGraph } from '../../../src/scenarios/plannedTopologyGraph.ts';
import type { PlannedTopologyGraph } from '../../../src/scenarios/plannedTopologyGraph.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';

const fixture = readFileSync(new URL('../../../../examples/scenarios/timeline-static.json', import.meta.url), 'utf8');

function source() {
  return parseScenario(fixture);
}

function resourceNode(projected: PlannedTopologyGraph, resource: string, slotIndex?: number) {
  const entry = [...projected.nodeResources].find(([, value]) => (
    value.resource === resource
    && (slotIndex === undefined ? value.slotIndex === undefined : BigInt(value.slotIndex ?? -1) === BigInt(slotIndex))
  ));
  assert.ok(entry, `${resource} configured node exists`);
  const node = projected.graph.nodes.find(item => item.id === entry[0]);
  assert.ok(node);
  return node;
}

test('planned graph keeps 50000 birth slots compact without runtime values or multiplied limits', () => {
  const input = source();
  const group = input.states['0'].workload.clientGroups[0];
  group.count = 50_000;
  group.overrides = [{
    index: 49_999,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: { bytes: 0 },
  }];
  const plan = projectPlannedTopology(validateScenario(input), 0n);
  const projected = projectPlannedTopologyGraph(plan);
  assert.equal(projected.graph.nodes.length, 1 + 2 + plan.backends.length);
  assert.equal(projected.graph.links.length, 2 * (2 + plan.backends.length));

  const defaults = resourceNode(projected, `group:${group.id}`);
  const paths = projected.graph.links.filter(link => link.source === defaults.id || link.target === defaults.id);
  assert.equal(paths.length, 2);
  for (const path of paths) {
    assert.equal(path.memberCount, 49_999);
    assert.equal(projected.linkDetails.get(path.id)?.memberCount, '49999');
  }
  const lb = resourceNode(projected, 'loadBalancer');
  assert.equal(lb.metrics.find(metric => metric.key === 'ingressBitsPerSecond')?.value,
    Number(plan.loadBalancer.ingress.bandwidthBitsPerSecond));
  assert.equal(lb.metrics.find(metric => metric.key === 'forwardingBitsPerSecond')?.value,
    Number(plan.loadBalancer.forwardingBitsPerSecond));
  assert.equal(projected.graph.ingress, undefined);
  for (const node of projected.graph.nodes) {
    assert.equal(node.active, undefined);
    assert.equal(node.traffic, undefined);
    assert.ok(node.metrics.every(metric => !/active|busy|utilization|queued|dropped|processed/i.test(metric.key)));
  }
  assert.ok(projected.graph.links.every(link => link.stats === undefined));
  assert.ok(JSON.stringify(projected.graph).length < 15_000);
});

test('planned arrows preserve both directional settings, resolved profiles, explicit zeros and exact source integers', () => {
  const input = source();
  input.fixed.network.profiles.push({
    id: 'special',
    config: { bandwidthBitsPerSecond: 7_000_000, packetTravelTimeNanoseconds: 123, queueCapacity: 32, lossRate: 0.25 },
  });
  const group = input.states['0'].workload.clientGroups[0];
  group.count = 2;
  group.network.toLoadBalancer = { bandwidthBitsPerSecond: 123_000 };
  group.network.fromLoadBalancer = { bandwidthBitsPerSecond: 321_000 };
  group.overrides = [{
    index: 0,
    network: {
      profile: 'special',
      toLoadBalancer: { bandwidthBitsPerSecond: '9007199254740993', packetTravelTimeNanoseconds: 0, queueCapacity: 0, lossRate: 0 },
      fromLoadBalancer: { bandwidthBitsPerSecond: 7_654 },
    },
    stream: {},
  }];
  const backend = input.states['0'].resources.backends[0];
  backend.network.toLoadBalancer = { bandwidthBitsPerSecond: 456_000 };
  backend.network.fromLoadBalancer = { bandwidthBitsPerSecond: 654_000 };
  const projected = projectPlannedTopologyGraph(projectPlannedTopology(validateScenario(input), 0n));
  const lb = resourceNode(projected, 'loadBalancer');

  for (const [resource, slot, toBandwidth, fromBandwidth] of [
    [`group:${group.id}`, undefined, 123_000, 321_000],
    [`group:${group.id}`, 0, Number('9007199254740993'), 7_654],
    [`backend:${backend.id}`, undefined, 456_000, 654_000],
  ] as const) {
    const endpoint = resourceNode(projected, resource, slot);
    const towardLB = projected.graph.links.find(link => link.source === endpoint.id && link.target === lb.id);
    const fromLB = projected.graph.links.find(link => link.source === lb.id && link.target === endpoint.id);
    assert.ok(towardLB);
    assert.ok(fromLB);
    assert.equal(towardLB.config.bandwidthBitsPerSecond, toBandwidth);
    assert.equal(fromLB.config.bandwidthBitsPerSecond, fromBandwidth);
  }

  const slot = resourceNode(projected, `group:${group.id}`, 0);
  const access = projected.graph.links.find(link => link.source === slot.id);
  const reply = projected.graph.links.find(link => link.target === slot.id);
  assert.ok(access);
  assert.ok(reply);
  assert.equal(access.config.packetTravelTimeMs, 0);
  assert.equal(access.config.queueCapacity, 0);
  assert.equal(access.config.lossRate, 0);
  assert.equal(reply.config.packetTravelTimeMs, 0.000123);
  assert.equal(reply.config.queueCapacity, 32);
  assert.equal(reply.config.lossRate, 0.25);
  assert.equal(projected.linkDetails.get(access.id)?.config.bandwidthBitsPerSecond, '9007199254740993');
});

test('budget decreases retain stable reserved slot selection without asserting historical client paths', () => {
  const input = source();
  const initial = input.states['0'];
  const group = initial.workload.clientGroups[0];
  group.count = 5;
  group.overrides = [{
    index: 4,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: { bytes: 0 },
  }];
  const smaller = structuredClone(initial);
  smaller.workload.clientGroups[0].count = 1;
  input.states['1000'] = smaller;
  const empty = structuredClone(smaller);
  empty.workload.clientGroups[0].count = 0;
  input.states['2000'] = empty;
  const checked = validateScenario(input);
  const before = projectPlannedTopologyGraph(projectPlannedTopology(checked, 0n));
  const after = projectPlannedTopologyGraph(projectPlannedTopology(checked, 1000n));
  const zero = projectPlannedTopologyGraph(projectPlannedTopology(checked, 2000n));
  const resource = `group:${group.id}`;
  const slotBefore = resourceNode(before, resource, 4);
  const slotAfter = resourceNode(after, resource, 4);
  assert.equal(slotAfter.id, slotBefore.id);
  assert.match(slotAfter.label, /reserved slot 4/);
  assert.deepEqual(after.nodeResources.get(slotAfter.id), { resource, slotIndex: 4 });
  assert.equal(before.graph.links.filter(link => link.source === slotBefore.id || link.target === slotBefore.id).length, 2);
  assert.equal(after.graph.links.filter(link => link.source === slotAfter.id || link.target === slotAfter.id).length, 0);

  const defaultsBefore = resourceNode(before, resource);
  const defaultsAfter = resourceNode(after, resource);
  assert.equal(defaultsAfter.id, defaultsBefore.id);
  const pathBefore = before.graph.links.find(link => link.source === defaultsBefore.id);
  const pathAfter = after.graph.links.find(link => link.source === defaultsAfter.id);
  assert.ok(pathBefore);
  assert.ok(pathAfter);
  assert.equal(pathAfter.id, pathBefore.id);
  assert.equal(before.linkDetails.get(pathBefore.id)?.memberCount, '4');
  assert.equal(after.linkDetails.get(pathAfter.id)?.memberCount, '1');
  assert.ok(resourceNode(zero, resource));
  const emptyGroupNodes = new Set(
    [...zero.nodeResources].filter(([, value]) => value.resource === resource).map(([id]) => id),
  );
  assert.equal(zero.graph.links.some(link => (
    emptyGroupNodes.has(link.source) || emptyGroupNodes.has(link.target)
  )), false);
});

test('view IDs distinguish resource roles and sparse slot nodes when canonical IDs match', () => {
  const plan = projectPlannedTopology(source(), 0n);
  plan.loadBalancer.id = 'same';
  plan.clientGroups[0].id = 'same';
  plan.backends[0].id = 'same';
  plan.clientGroups[0].overrides = [{
    index: '0', reserved: false,
    network: structuredClone(plan.clientGroups[0].network),
    stream: structuredClone(plan.clientGroups[0].stream),
  }];
  const projected = projectPlannedTopologyGraph(plan);
  const ids = [...projected.graph.nodes, ...projected.graph.links].map(item => item.id);
  assert.equal(new Set(ids).size, ids.length);
  assert.equal(new Set([
    resourceNode(projected, 'loadBalancer').id,
    resourceNode(projected, 'group:same').id,
    resourceNode(projected, 'group:same', 0).id,
    resourceNode(projected, 'backend:same').id,
  ]).size, 4);
  assert.equal(resourceNode(projected, 'group:same', 0).resourceId, 'same');
});

test('graph projection is pure and does not retain mutable exact inspector configuration', () => {
  const plan = projectPlannedTopology(source(), 0n);
  const before = structuredClone(plan);
  const projected = projectPlannedTopologyGraph(plan);
  assert.deepEqual(plan, before);
  assert.deepEqual(projectPlannedTopologyGraph(plan), projected);
  for (const link of projected.graph.links) link.config.queueCapacity = -1;
  for (const details of projected.linkDetails.values()) {
    details.config.lossRate = 0.75;
    details.config.bandwidthBitsPerSecond = '9007199254740993';
  }
  for (const node of projected.graph.nodes) {
    node.label = 'changed';
    for (const metric of node.metrics) metric.value = -1;
  }
  assert.deepEqual(plan, before);
});
