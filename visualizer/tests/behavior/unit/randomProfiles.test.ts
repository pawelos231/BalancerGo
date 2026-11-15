import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  parseRandomProfile,
  serializeRandomProfile,
  validateRandomProfile,
} from '../../../src/scenarios/randomness.ts';
import type { RandomProfile } from '../../../src/scenarios/randomness.ts';

const corpus = JSON.parse(readFileSync(
  new URL('../../../../examples/randomness/profile-cases.json', import.meta.url), 'utf8',
)) as { cases: { name: string; valid: boolean; document: string }[] };

test('random profiles follow the shared Go and TypeScript contract corpus', () => {
  for (const entry of corpus.cases) {
    if (entry.valid) {
      const profile = parseRandomProfile(entry.document);
      assert.deepEqual(profile, JSON.parse(entry.document), entry.name);
      assert.deepEqual(parseRandomProfile(serializeRandomProfile(profile)), profile, entry.name);
    } else {
      assert.throws(() => parseRandomProfile(entry.document), undefined, entry.name);
    }
  }
});

test('uniform bounds retain exact ordering above safe integers and at tiny fractions', () => {
  for (const [unit, min, max] of [
    ['number', '9007199254740992', '9007199254740993'],
    ['nanoseconds', '9223372036854775806.999999999999999999', '9223372036854775807'],
    ['bytes', '0', `0.${'0'.repeat(125)}1`],
    ['percentage', '99.999999999999999999999999999999999999', '100'],
  ] as const) {
    const expected = { kind: 'uniform', unit, min, max } as const;
    assert.deepEqual(validateRandomProfile(expected), expected);
    assert.throws(() => validateRandomProfile({ ...expected, min: max, max: min }), /less than/);
  }
});

test('validation returns independent copies and serialization fixes field order', () => {
  const input = { value: '0.125', unit: 'bytes', kind: 'constant' };
  const profile = validateRandomProfile(input);
  assert.notEqual(profile, input);
  input.value = '9';
  assert.deepEqual(profile, { kind: 'constant', unit: 'bytes', value: '0.125' });
  assert.equal(serializeRandomProfile(profile), '{"kind":"constant","unit":"bytes","value":"0.125"}');
  assert.equal(serializeRandomProfile(validateRandomProfile({ max: '5', min: '1', unit: 'number', kind: 'uniform' })),
    '{"kind":"uniform","unit":"number","min":"1","max":"5"}');
});

test('object validation and serialization reject malformed runtime inputs', () => {
  const inherited = Object.create({ kind: 'constant' }) as Record<string, unknown>;
  inherited.unit = 'bytes';
  inherited.value = '1';
  const valid = { kind: 'constant', unit: 'bytes', value: '1' };
  for (const input of [
    null, [], true, 'constant', inherited,
    { ...valid, extra: undefined },
    { ...valid, [Symbol('extra')]: '1' },
    { ...valid, value: 1 },
    { ...valid, value: '1\n' },
    { ...valid, unit: null },
    { ...valid, kind: 'Constant' },
  ]) {
    assert.throws(() => validateRandomProfile(input));
    assert.throws(() => serializeRandomProfile(input as RandomProfile));
  }
});
