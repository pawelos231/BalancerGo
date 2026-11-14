import assert from 'node:assert/strict';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

const base = 'run 10s\nbalancer lb {}\n';

test('explicit backend addresses preserve native identity without changing automatic allocation', () => {
  const explicit = compileScenario(`${base}
backend backend-1 { name "Backend 1" address "10.0.0.1" }
backend backend-2 { name "Backend 2" address "10.0.0.2" }
backend backend-10 { name "Backend 10" address "10.0.0.10" }
at 1s {
    add backend backend-3 { name "Backend 3" address "10.0.0.3" }
}
at 2s { set backend backend-2 link 10MB }
`);
  const initial = explicit.states['0'].resources.backends;
  assert.deepEqual(initial.map(({ id, name, address, port }) => ({ id, name, address, port })), [
    { id: 'backend-1', name: 'Backend 1', address: '10.0.0.1', port: 80 },
    { id: 'backend-2', name: 'Backend 2', address: '10.0.0.2', port: 80 },
    { id: 'backend-10', name: 'Backend 10', address: '10.0.0.10', port: 80 },
  ]);
  assert.equal(explicit.states['1000000000'].resources.backends[3].address, '10.0.0.3');
  assert.equal(explicit.states['2000000000'].resources.backends[1].address, '10.0.0.2');

  const automatic = compileScenario(`${base}backends backend { count 100 from 1 }`);
  const addresses = new Map(automatic.states['0'].resources.backends.map(backend => [backend.id, backend.address]));
  assert.equal(addresses.get('backend-1'), '10.0.0.1');
  assert.equal(addresses.get('backend-100'), '10.0.0.3');
  assert.equal(addresses.get('backend-2'), '10.0.0.13');
});

test('explicit backend addresses retain endpoint validation and cannot change after insertion', () => {
  const backend = `${base}backend api-0 { address "10.0.0.9" }\n`;
  assert.throws(() => compileScenario(`${backend}backend api-1 { address "10.0.0.9" }`), /endpoint.*already used/);
  assert.throws(() => compileScenario(`${base}
backend api-0 { address "10.0.0.2" }
backend api-1 {}
`), /endpoint.*already used/);
  assert.throws(() => compileScenario(`${backend}at 1s { add backend api-1 { address "10.0.0.9" } }`), /endpoint.*already used/);
  assert.throws(() => compileScenario(`${backend}at 1s { set backend api-0 address "10.0.0.10" }`), /Unknown setting "address"/);
  assert.throws(() => compileScenario(`${base}at 1s {
    add backend api-0 { address "10.0.0.9" }
    set backend api-0 address "10.0.0.10"
}`), /Unknown setting "address"/);
  assert.throws(() => compileScenario(`${base}backend api-0 { address null }`), /double-quoted text/);

  const invalid = `${base}backend api-0 { address "bad host" }`;
  assert.throws(() => compileScenario(invalid), error => {
    assert.ok(error instanceof LanguageError);
    assert.match(error.message, /nonempty host without whitespace/);
    assert.equal(invalid.slice(error.range.start.offset, error.range.end.offset), '"bad host"');
    return true;
  });
});

test('numeric group segments keep native IDs and client aliases across declarations and timeline commands', () => {
  const scenario = compileScenario(`${base}
clients wave-0s { count 2 }
client wave-0s-0 { payload 1B }
at 1s {
    set clients wave-0s every 1ms
    set client wave-0s-1 payload 2B
    add clients wave-5s { count 1 }
    set client wave-5s-0 payload 3B
}
at 2s { stop arrivals wave-0s }
at 3s { remove clients wave-5s }
`);
  assert.equal(scenario.states['0'].workload.clientGroups[0].id, 'wave-0s');
  const changed = scenario.states['1000000000'].workload.clientGroups;
  assert.equal(changed[0].clientGapNanoseconds, 1_000_000);
  assert.deepEqual(changed[0].overrides!.map(override => [override.index, override.stream.bytes]), [[0, 1], [1, 2]]);
  assert.equal(changed[1].id, 'wave-5s');
  assert.equal(changed[1].overrides![0].stream.bytes, 3);
  assert.equal(scenario.states['2000000000'].workload.clientGroups[0].count, 0);
  assert.deepEqual(scenario.states['3000000000'].workload.clientGroups.map(group => group.id), ['wave-0s']);
});

test('numeric workload names retain alias bounds and collisions without relaxing other resource names', () => {
  assert.throws(() => compileScenario(`${base}backend wave-0 {}\nclients wave-0 {}`), /already used/);
  assert.throws(() => compileScenario(`${base}backend api-0 {}\nclients api {}`), /client alias/);
  assert.throws(() => compileScenario(`${base}clients wave-0s {}\nclient wave-0s-1 {}`), /outside its declared group/);
  assert.throws(() => compileScenario(`${base}clients wave-0s {}\nclient wave-0s-01 {}`), /client alias/);
  assert.throws(() => compileScenario(`${base}clients wave2 {}`), /client group name/);
  assert.throws(() => compileScenario(`${base}backend wave-0s-0 {}`), /endpoint name/);
  assert.throws(() => compileScenario(`${base}profile wave-0s {}`), /lowercase name/);
  assert.throws(() => compileScenario('run 10s\nbalancer wave-0s {}'), /lowercase name/);
  assert.throws(() => compileScenario(`${base}backends wave-0s { count 1 }`), /lowercase name/);
});

test('removing client groups preserves retained order, histories and later indexed edits', () => {
  const scenario = compileScenario(`${base}
clients first { count 1 }
clients middle { count 2 }
clients last { count 2 }
at 1s {
    set clients middle count 1
    remove clients first
    set client middle-1 payload 2B
    set clients last every 1ms
    add clients added { count 1 }
    set client added-0 payload 3B
}
at 2s {
    remove clients middle
    set client last-1 payload 4B
    set clients added every 2ms
}
`);
  assert.deepEqual(scenario.states['0'].workload.clientGroups.map(group => group.id), ['first', 'middle', 'last']);
  const first = scenario.states['1000000000'].workload.clientGroups;
  assert.deepEqual(first.map(group => group.id), ['middle', 'last', 'added']);
  assert.equal(first[0].overrides![0].index, 1);
  assert.equal(first[0].overrides![0].stream.bytes, 2);
  assert.equal(first[1].clientGapNanoseconds, 1_000_000);
  const second = scenario.states['2000000000'].workload.clientGroups;
  assert.deepEqual(second.map(group => group.id), ['last', 'added']);
  assert.equal(second[0].overrides![0].stream.bytes, 4);
  assert.equal(second[1].overrides![0].stream.bytes, 3);
  assert.equal(second[1].startAtNanoseconds, 1_000_000_000);
  assert.equal(second[1].clientGapNanoseconds, 2_000_000);
});

test('client removal rejects missing resources, repeated removal, later references and ID reuse', () => {
  const groups = `${base}clients first {}\nclients second {}\n`;
  for (const commands of [
    'remove clients missing',
    'remove clients first\nremove clients first',
    'remove clients first\nset clients first count 2',
    'remove clients first\nset client first-0 payload 1B',
    'remove clients first\nstop arrivals first',
  ]) {
    assert.throws(() => compileScenario(`${groups}at 1s {\n${commands}\n}`), /Unknown client group/);
  }
  assert.throws(() => compileScenario(`${groups}at 1s {
    remove clients first
    add clients first {}
}`), /cannot be reused after removal/);
  assert.throws(() => compileScenario(`${groups}
at 1s { remove clients first }
at 2s { add clients first {} }
`), /cannot be reused after removal/);
  assert.throws(() => compileScenario(`${groups}at 1s { remove backend api-0 }`), /Expected clients/);

  assert.throws(() => compileScenario(`${groups}at 1s {
    set clients second count 2
    remove clients first
    set clients second count 3
}`), /overlaps/);
});

test('compacting client groups remaps new and inherited diagnostic origins', () => {
  const source = `${base}
clients first {}
clients second {}
at 1s {
    remove clients first
    set clients second count 16777215
}
`;
  assert.throws(() => compileScenario(source), error => {
    assert.ok(error instanceof LanguageError);
    assert.match(error.message, /total reserved clients/);
    assert.equal(source.slice(error.range.start.offset, error.range.end.offset), '16777215');
    return true;
  });

  const inherited = `${base}
clients first {}
clients second { packets 9007199254740992 payload 1B }
at 1s { remove clients first }
at 2s { set clients second payload -1 }
`;
  assert.throws(() => compileScenario(inherited), error => {
    assert.ok(error instanceof LanguageError);
    assert.match(error.message, /stream size overflows/);
    assert.equal(inherited.slice(error.range.start.offset, error.range.end.offset), '9007199254740992');
    return true;
  });
});

test('default queue policy preserves the native empty spelling while tail-drop remains the language default', () => {
  const original = compileScenario(base);
  assert.equal(original.fixed.network.defaults.queuePolicy, 'tailDrop');
  assert.equal(original.states['0'].resources.loadBalancer.ingress.queuePolicy, 'tailDrop');
  const scenario = compileScenario(`${base.replace('balancer lb {}', 'balancer lb { ingress { queue-policy default } }')}
defaults queue-policy default signal-probability 0%
backend api-0 { queue-policy default }
clients regular { to-lb { queue-policy default } }
`);
  assert.equal(scenario.fixed.network.defaults.queuePolicy, '');
  assert.equal(scenario.fixed.network.defaults.congestionSignalProbability, 0);
  assert.equal(scenario.states['0'].resources.loadBalancer.ingress.queuePolicy, '');
  assert.equal(scenario.states['0'].resources.backends[0].network.toLoadBalancer.queuePolicy, '');
  assert.equal(scenario.states['0'].workload.clientGroups[0].network.toLoadBalancer.queuePolicy, '');
});
