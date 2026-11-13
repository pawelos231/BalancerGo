import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { parseScenario } from '../../../src/scenarios/validation.ts';

const fixture = (name: string) => readFileSync(new URL(`../../../../examples/language/${name}`, import.meta.url), 'utf8');
const minimal = 'run 1s\nbalancer lb {}\nbackend api-0 {}';

// The documented source must produce the documented native JSON, including pinned TCP defaults.
test('language compiler emits the exact validated static specification fixture', () => {
  assert.deepEqual(compileScenario(fixture('basic.lb')), parseScenario(fixture('basic.json')));
});

// Declaration order does not change endpoint addresses, and every compilation owns its data.
test('language static compilation preserves exact controls and deterministic backend identity', () => {
  const source = 'language 1\nrun 1s seed -9007199254740993 max-events 18446744073709551615 speed 0\nbalancer lb {}\n';
  const first = compileScenario(`${source}backend api-1 {}\nbackend api-0 {}`);
  const second = compileScenario(`${source}backend api-0 {}\nbackend api-1 {}`);
  const addresses = (scenario: typeof first) => Object.fromEntries(
    scenario.states['0'].resources.backends.map(backend => [backend.id, backend.address]),
  );
  assert.deepEqual(addresses(first), addresses(second));
  assert.equal(first.fixed.execution.seed, '-9007199254740993');
  assert.equal(first.fixed.execution.maxEvents, '18446744073709551615');
  first.fixed.network.defaults.queueCapacity = 0;
  assert.equal(second.fixed.network.defaults.queueCapacity, 1024);
  assert.equal(second.states['0'].workload.clientGroups.length, 0);
});

// Recognized syntax cannot vanish silently while its later semantic step is unfinished.
test('language static compiler rejects invalid identities, duplicates and unavailable features', () => {
  assert.throws(() => compileScenario(`${minimal}\nbackend api-0 {}`), /Duplicate backend/);
  assert.throws(() => compileScenario(minimal.replace('api-0', 'api-01')), /endpoint name/);
  assert.throws(() => compileScenario(`${minimal}\nbalancer another {}`), /Only one balancer/);
  assert.throws(() => compileScenario('run 1s'), /one balancer/);
  assert.throws(() => compileScenario(`${minimal}\nclients regular {}`), /not available yet/);
  assert.throws(() => compileScenario(`${minimal}\nat 1ms {}`), /Timeline compilation/);
  assert.throws(() => compileScenario(`language 2\n${minimal}`), /between 1 and 1/);
  assert.throws(() => compileScenario(minimal.replace('lb {}', 'lb { algo p2c }')), /Expected one of/);
});
