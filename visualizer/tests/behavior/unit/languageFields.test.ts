import assert from 'node:assert/strict';
import test from 'node:test';
import { scenarioDefaults } from '../../../src/scenario-language/defaults.ts';
import { FieldWriter } from '../../../src/scenario-language/fields.ts';
import type { FieldRules } from '../../../src/scenario-language/fields.ts';
import { parse } from '../../../src/scenario-language/parser.ts';
import { countValue, timeValue } from '../../../src/scenario-language/values.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';

const rules: FieldRules = {
  count: { path: 'count', read: countValue, nullable: true },
  total: { path: 'count', read: countValue },
  rto: { path: 'rto', fields: { minimum: { path: 'minimum', read: timeValue } } },
};
const properties = (source: string) => parse(`tcp { ${source} }`).declarations[0].properties;

// Explicit zero is kept; null is a separate inheritance instruction, never mistaken for omission.
test('language field rules preserve omitted, zero and null and track source origins', () => {
  const writer = new FieldWriter();
  const target = { count: 7 };
  writer.apply(properties(''), rules, target, 'test');
  assert.equal(target.count, 7);
  writer.apply(properties('count 0'), rules, target, 'test');
  assert.equal(target.count, 0);
  assert.equal(writer.locations.get('test.count')?.start.column, 13);

  const inherited: Record<string, unknown> = {};
  new FieldWriter().apply(properties('count null rto { minimum 200ms }'), rules, inherited, 'test');
  assert.deepEqual(inherited, { count: null, rto: { minimum: 200_000_000 } });
});

// Duplicate names and aliases are errors, including attempts to look up inherited JS properties.
test('language field rules reject unknown names, duplicate destinations and wrong shapes', () => {
  for (const source of ['count 1 count 2', 'count 1 total 2', 'count 1 constructor 2']) {
    assert.throws(() => new FieldWriter().apply(properties(source), rules, {}, ''), /Duplicate|overlaps|Unknown/);
  }
  assert.throws(() => new FieldWriter().apply(properties('count {}'), rules, {}, ''), /scalar/);
  assert.throws(() => new FieldWriter().apply(properties('rto 1'), rules, {}, ''), /block/);
});

// Language defaults are complete and independent; no running-server settings are consulted.
test('language version one defaults validate and cannot leak changes between compilations', () => {
  const first = scenarioDefaults(180_000_000_000);
  validateScenario(first);
  first.fixed.tcp.rto!.minimumTimeoutNanoseconds = 1;
  first.fixed.network.defaults.queueCapacity = 0;
  const second = scenarioDefaults(180_000_000_000);
  assert.equal(second.fixed.tcp.rto?.minimumTimeoutNanoseconds, 200_000_000);
  assert.equal(second.fixed.network.defaults.queueCapacity, 1024);
  assert.equal(second.fixed.execution.maxEvents, 5_000_000_000);
});
