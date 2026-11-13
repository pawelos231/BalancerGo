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

// Even a future ID sorted before the initial backend is reserved before state zero is built.
test('language timeline reserves future backend identities and supports add then set atomically', () => {
  const result = compileScenario(`run 10s
balancer lb {}
backend zebra-0 {}
at 1s {
    add backends api { count 2 link 10MB }
    set backend api-0 link 20MB
}
at 2s { set backend zebra-0 host { packet-time 10us } }
`);
  const initial = result.states['0'].resources.backends;
  assert.equal(initial.length, 1);
  assert.equal(initial[0].address, '10.0.0.3');
  const added = result.states['1000000000'].resources.backends;
  assert.deepEqual(added.map(backend => backend.id), ['zebra-0', 'api-0', 'api-1']);
  assert.equal(added[1].network.toLoadBalancer.bandwidthBitsPerSecond, 160_000_000);
  assert.equal(added[2].network.toLoadBalancer.bandwidthBitsPerSecond, 80_000_000);
  assert.equal(result.states['2000000000'].resources.backends[0].address, initial[0].address);
});

// A reference must follow its addition, while IDs and counts are reserved across the whole source.
test('language timeline backend additions reject duplicate IDs, premature references and global excess', () => {
  assert.throws(() => compileScenario(`${base}at 1s { add backend api-0 {} }`), /Duplicate backend/);
  assert.throws(() => compileScenario(`${base}at 1s {
    set backend added-0 link 1MB
    add backend added-0 {}
  }`), /Unknown backend/);
  assert.throws(() => compileScenario(`${base}at 1s { add backends extra { count 10000 } }`), /10,000 backends/);
});

// New generators begin at insertion time; later states retain their original start and lifetime budget.
test('language timeline adds compact client groups without restarting retained generators', () => {
  const result = compileScenario(`${base}
at 1s {
    add clients burst { count 3 payload 1KB every 10ms }
    set client burst-1 payload 2KB
}
at 2s {
    set balancer egress 100MB
    add clients later { count 1 start 3s }
}`);
  assert.equal(result.states['0'].workload.clientGroups.length, 1);
  const first = result.states['1000000000'].workload.clientGroups;
  assert.equal(first[0].startAtNanoseconds, 0);
  assert.equal(first[1].id, 'burst');
  assert.equal(first[1].startAtNanoseconds, 1_000_000_000);
  assert.equal(first[1].overrides![0].index, 1);
  const second = result.states['2000000000'].workload.clientGroups;
  assert.equal(second[1].startAtNanoseconds, first[1].startAtNanoseconds);
  assert.equal(second[1].count, 3);
  assert.equal(second[2].startAtNanoseconds, 3_000_000_000);
});

// Groups cannot appear in the past or reuse an ID; reducing a budget never frees reserved slots.
test('language timeline client additions reject past starts, duplicates and exhausted reservations', () => {
  assert.throws(() => compileScenario(`${base}at 1s { add clients regular {} }`), /Duplicate client group/);
  assert.throws(() => compileScenario(`${base}at 1s { add clients past { start 0ns } }`), /at or after/);
  const full = base.replace('count 4', 'count 16777215');
  assert.throws(() => compileScenario(`${full}at 1s {
    set clients regular count 0
    add clients extra { count 1 }
  }`), /total reserved clients/);
  assert.throws(() => compileScenario(`${base}at 1s {
    set clients burst count 2
    add clients burst {}
  }`), /Unknown client group/);
});
