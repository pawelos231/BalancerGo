import assert from 'node:assert/strict';
import test from 'node:test';
import { buildDensityPreview } from '../../../src/docs/densityPreview.ts';
import type { RandomProfile } from '../../../src/scenarios/randomness.ts';

// [5000,15000) ns has width 10000 ns and analytical density 1/10000 per ns.
test('uniform preview shows exact density, units and the complete padded axis', () => {
  const profile: RandomProfile = { kind: 'uniform', unit: 'nanoseconds', min: '5000', max: '15000' };
  assert.deepEqual(buildDensityPreview(profile), {
    kind: 'uniform', unitLabel: 'ns', minimum: '5000', maximum: '15000',
    axisMinimum: '2500', axisMaximum: '17500', density: '1 / 10000',
  });
  assert.deepEqual(profile, { kind: 'uniform', unit: 'nanoseconds', min: '5000', max: '15000' });
});

// A density of 1/2 over width 2 has total area 1; equal-width intervals have equal mass.
test('fractional density remains an exact reciprocal and axes may show zero-density space', () => {
  assert.deepEqual(buildDensityPreview({ kind: 'uniform', unit: 'number', min: '0.5', max: '2.5' }), {
    kind: 'uniform', unitLabel: 'unit', minimum: '0.5', maximum: '2.5',
    axisMinimum: '0', axisMaximum: '3', density: '1 / 2',
  });
  const repeating = buildDensityPreview({ kind: 'uniform', unit: 'percentage', min: '0', max: '3' });
  assert.equal(repeating.kind === 'uniform' && repeating.density, '1 / 3');
  assert.equal(repeating.axisMinimum, '-0.75');
  assert.equal(repeating.unitLabel, '%');
});

// These bounds collapse through Number; normalized drawing must still retain their width of 0.25 B.
test('large fractional bounds and small spans never pass through floating point', () => {
  assert.deepEqual(buildDensityPreview({
    kind: 'uniform', unit: 'bytes', min: '9007199254740992.125', max: '9007199254740992.375',
  }), {
    kind: 'uniform', unitLabel: 'B',
    minimum: '9007199254740992.125', maximum: '9007199254740992.375',
    axisMinimum: '9007199254740992.0625', axisMaximum: '9007199254740992.4375', density: '4',
  });
  const tiny = buildDensityPreview({
    kind: 'uniform', unit: 'nanoseconds', min: '0.00000000000000000001', max: '0.00000000000000000003',
  });
  assert.equal(tiny.axisMinimum, '0.000000000000000000005');
  assert.equal(tiny.axisMaximum, '0.000000000000000000035');
});

// A fixed value is one point, not a rectangle or an invented finite-density spike.
test('constant preview exposes a single value without a uniform density', () => {
  assert.deepEqual(buildDensityPreview({ kind: 'constant', unit: 'nanoseconds', value: '0' }), {
    kind: 'constant', unitLabel: 'ns', value: '0', axisMinimum: '-1', axisMaximum: '1',
  });
});

// Preparation keeps the same admission rules as the real sampler definition.
test('preview rejects equal, reversed and malformed bounds', () => {
  for (const profile of [
    { kind: 'uniform', unit: 'number', min: '1', max: '1' },
    { kind: 'uniform', unit: 'number', min: '2', max: '1' },
    { kind: 'uniform', unit: 'number', min: '0', max: '1e3' },
  ] as const) {
    assert.throws(() => buildDensityPreview(profile));
  }
});
