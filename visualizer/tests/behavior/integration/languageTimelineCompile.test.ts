import assert from 'node:assert/strict';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';

const base = `run 10s
balancer lb {}
backend api-0 {}
clients regular { count 4 }
`;

// All edits at one timestamp produce one independent full state, preserving earlier history.
test('language timeline set updates LB, backend, groups and historically reserved client slots', () => {
  const result = compileScenario(`${base}
at 1s {
    set balancer ingress 100MB
    set balancer host { cores 2 }
    set backend api-0 { from-lb { link 10MB } host { packet-time 10us } }
}
at 2s {
    set clients regular count 2 every 10ms
    set client regular-3 payload 2KB
}
`);
  assert.deepEqual(Object.keys(result.states), ['0', '1000000000', '2000000000']);
  assert.equal(result.states['0'].resources.loadBalancer.ingress.bandwidthBitsPerSecond, 1_000_000_000);
  const first = result.states['1000000000'];
  assert.equal(first.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 800_000_000);
  assert.equal(first.resources.backends[0].host?.packetProcessingTimeNanoseconds, 10_000);
  assert.equal(first.workload.clientGroups[0].count, 4);
  const last = result.states['2000000000'].workload.clientGroups[0];
  assert.equal(last.count, 2);
  assert.equal(last.overrides![0].index, 3);
  assert.equal(last.overrides![0].stream.bytes, 2000);
});

// Aliases and shorthand writes cannot target the same native leaf twice in one atomic block.
test('language timeline set rejects conflicting writes and fixed or missing resources', () => {
  assert.throws(() => compileScenario(`${base}at 1s {
    set backend api-0 link 1MB
    set backend api-0 to-lb { link 2MB }
  }`), /overlaps/);
  assert.throws(() => compileScenario(`${base}at 1s { set backend absent-0 link 1MB }`), /Unknown backend/);
  assert.throws(() => compileScenario(`${base}at 1s { set clients regular start 2s }`), /Unknown setting/);
  assert.throws(() => compileScenario(`${base}at 1s { set backend api-0 address "changed" }`), /Unknown setting/);
  assert.throws(() => compileScenario(`${base}at 1s { set client regular-4 payload 1KB }`), /identify a client/);
  assert.throws(() => compileScenario(`${base}at 1s { set balancer {} }`), /at least one setting/);
});

// Timeline order and domains are checked before returning any document.
test('language timeline compilation validates chronology and future alias collisions', () => {
  const unordered = `${base}at 2s { set balancer egress 1MB }\nat 1s { set balancer egress 2MB }`;
  assert.throws(() => compileScenario(unordered), /increase strictly/);
  assert.throws(() => compileScenario(`${base}at 10s { set balancer egress 1MB }`), /precede the run end/);
  const collision = 'run 10s\nbalancer lb {}\nbackend regular-1 {}\nclients regular { count 0 }\n';
  assert.throws(() => compileScenario(`${collision}at 1s { set clients regular count 2 }`), /client alias/);
});

// Repeated edits reuse existing sparse entries even when insertion order differs from slot order.
test('language timeline reuses indexed overrides across a large sparse update block', () => {
  const updates = Array.from({ length: 498 }, (_, index) => {
    return `set client regular-${index + 1} payload 1KB`;
  }).join('\n');
  const source = `${base.replace('count 4', 'count 500')}
client regular-0 { payload 1KB }
at 1s {
    set client regular-0 packets 2
    set client regular-499 payload 2KB
    ${updates}
    set client regular-0 segment-gap 2ms
    set client regular-499 segment-gap 1ms
}`;
  const overrides = compileScenario(source).states['1000000000'].workload.clientGroups[0].overrides!;
  assert.equal(overrides.length, 500);
  assert.equal(overrides[0].index, 0);
  assert.deepEqual(overrides[0].stream, { bytes: 1000, packets: 2, packetGapNanoseconds: 2_000_000 });
  assert.equal(overrides[1].index, 499);
  assert.deepEqual(overrides[1].stream, { bytes: 2000, packetGapNanoseconds: 1_000_000 });
});
