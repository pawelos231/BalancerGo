import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { checkScenario } from '../../../src/scenarios/timelineValidation.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

const corpus = JSON.parse(readFileSync(
  new URL('../../../../examples/randomness/profile-cases.json', import.meta.url), 'utf8',
)) as { cases: { name: string; valid: boolean; document: string }[] };

const baseDocument = JSON.stringify(scenarioFixture());

// Embed raw definitions so duplicate keys and numeric tokens reach the native decoder intact.
function documentWithProfiles(profiles: string): string {
  return baseDocument.replace('"fixed":{', `"fixed":{"randomProfiles":${profiles},`);
}

for (const entry of corpus.cases) {
  test(`native scenario random profile: ${entry.name}`, () => {
    const document = documentWithProfiles(`{"sample":${entry.document}}`);
    if (entry.valid) {
      const scenario = parseScenario(document);
      assert.deepEqual(scenario.fixed.randomProfiles?.sample, JSON.parse(entry.document));
      assert.deepEqual(parseScenario(serializeScenario(scenario)), scenario);
    } else {
      assert.throws(() => parseScenario(document));
    }
  });
}

test('random profile maps reject null, non-objects, missing definitions and duplicate decoded keys', () => {
  const profile = '{"kind":"constant","unit":"number","value":"1"}';
  for (const profiles of [
    'null', '[]', '1', 'true', '"profile"', '{"sample":null}', '{"sample":{}}',
    `{"sample":${profile},"sample":${profile}}`,
    `{"sample":${profile},"s\\u0061mple":${profile}}`,
  ]) {
    assert.throws(() => parseScenario(documentWithProfiles(profiles)), /randomProfiles/);
  }
});

test('every random profile name follows native scenario ID rules', () => {
  const profile = { kind: 'constant', unit: 'number', value: '1' };
  for (const id of ['', 'contains space', 'cpu/time', 'żółw', 'x'.repeat(129)]) {
    const document = documentWithProfiles(JSON.stringify({ [id]: profile }));
    assert.throws(() => parseScenario(document), /fixed\.randomProfiles.*id/);
  }
  for (const id of ['CPU_1.delay', 'x'.repeat(128), '__proto__']) {
    const parsed = parseScenario(documentWithProfiles(JSON.stringify({ [id]: profile })));
    assert.equal(parsed.fixed.randomProfiles?.[id].kind, 'constant');
  }
});

test('native profile decoding preserves exact strings and returns detached maps and definitions', () => {
  const input = scenarioFixture();
  input.fixed.randomProfiles = {
    fractional: {
      kind: 'uniform', unit: 'nanoseconds',
      min: '9223372036854775806.999999999999999999', max: '9223372036854775807',
    },
    fixed: { kind: 'constant', unit: 'bytes', value: '9007199254740993' },
  };
  const original = structuredClone(input);
  const checked = validateScenario(input);
  assert.notEqual(checked.fixed.randomProfiles, input.fixed.randomProfiles);
  assert.notEqual(checked.fixed.randomProfiles?.fractional, input.fixed.randomProfiles.fractional);
  assert.deepEqual(checked.fixed.randomProfiles?.fractional, original.fixed.randomProfiles?.fractional);
  assert.deepEqual(parseScenario(serializeScenario(checked)), checked);

  checked.fixed.randomProfiles!.fixed = { kind: 'constant', unit: 'bytes', value: '0' };
  checked.fixed.randomProfiles!.fractional = { kind: 'uniform', unit: 'number', min: '0', max: '1' };
  assert.deepEqual(input, original);
  input.fixed.randomProfiles.fixed = { kind: 'constant', unit: 'number', value: '2' };
  assert.equal(checked.fixed.randomProfiles!.fixed.unit, 'bytes');
});

test('semantic validation checks unused random profile definitions and their names', () => {
  const input = scenarioFixture();
  input.fixed.randomProfiles = { unused: { kind: 'uniform', unit: 'number', min: '2', max: '1' } };
  assert.throws(() => checkScenario(input), /fixed\.randomProfiles\["unused"\].*less than/);
  input.fixed.randomProfiles = { 'bad name': { kind: 'constant', unit: 'number', value: '1' } };
  assert.throws(() => checkScenario(input), /fixed\.randomProfiles\["bad name"\].*id/);
});

test('legacy documents keep an absent random profile map while an explicit empty map survives', () => {
  const legacy = parseScenario(baseDocument);
  assert.equal(Object.hasOwn(legacy.fixed, 'randomProfiles'), false);
  const serialized = serializeScenario(legacy);
  assert.equal(serialized.includes('randomProfiles'), false);
  assert.equal(serializeScenario(parseScenario(serialized)), serialized);

  const explicit = parseScenario(documentWithProfiles('{}'));
  assert.equal(Object.hasOwn(explicit.fixed, 'randomProfiles'), true);
  assert.deepEqual(Object.keys(explicit.fixed.randomProfiles!), []);
  assert.equal(Object.hasOwn(parseScenario(serializeScenario(explicit)).fixed, 'randomProfiles'), true);
});

test('admitting random profiles keeps unknown fixed fields invalid', () => {
  const document = baseDocument.replace('"fixed":{', '"fixed":{"randomProfile":{},');
  assert.throws(() => parseScenario(document), /scenario\.fixed\.randomProfile: unknown field/);
});
