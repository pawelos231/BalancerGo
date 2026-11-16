import assert from 'node:assert/strict';
import test from 'node:test';
import { buildDistributionPreview } from '../../../src/docs/distributionPreview.ts';
import type { ContinuousPreview, DistributionPreview } from '../../../src/docs/distributionPreview.ts';
import type { RandomKind, RandomProfile } from '../../../src/scenarios/randomness.ts';
import { familyByKind } from '../../../src/scenarios/randomness/registry.ts';

// The shared model lands with Normal; later positive oracles become active at
// their declaration stage. A fully activated registry runs every oracle.
function previewTest(name: string, kinds: readonly RandomKind[], run: () => void): void {
  const missing = kinds.filter(kind => !familyByKind(kind));
  test(name, { skip: missing.length > 0 ? `Awaiting declaration stages: ${missing.join(', ')}` : false }, run);
}

function close(actual: number, expected: number, tolerance = 2e-10): void {
  assert.ok(Math.abs(actual - expected) <= tolerance * Math.max(1, Math.abs(expected)), `${actual} ≠ ${expected}`);
}

function continuous(profile: RandomProfile): ContinuousPreview {
  const preview = buildDistributionPreview(profile);
  assert.equal(preview.kind, 'pdf');
  return preview as ContinuousPreview;
}

function densityAt(preview: ContinuousPreview, value: number): number {
  const position = (value - Number(preview.minimum)) / (Number(preview.maximum) - Number(preview.minimum));
  const point = preview.points.find(point => Math.abs(point.position - position) < 1e-12);
  assert.ok(point, `The analytical grid should include ${value}.`);
  return point.density;
}

// Oracles below use elementary special cases, independently of jStat:
// Gamma(1, theta) and Weibull(1, lambda) are exponential; t(1) is Cauchy.
// The normal interval mass is the high-precision value Phi(2) - Phi(-2).
previewTest('normal is normalized over the configured interval rather than the full real line', ["normal"], () => {
  const preview = continuous({ kind: 'normal', unit: 'number', mean: '2', stddev: '1', min: '0', max: '4' });
  close(preview.normalizationMass, 0.9544997361036416);
  close(densityAt(preview, 2), 1 / Math.sqrt(2 * Math.PI) / 0.9544997361036416);
  assert.equal(preview.minimum, '0');
  assert.equal(preview.maximum, '4');
});

previewTest('exponential mean defines the conditional density', ['exponential'], () => {
  const preview = continuous({ kind: 'exponential', unit: 'number', mean: '2', min: '1', max: '5' });
  const expected = (Math.exp(-1.5) / 2) / (Math.exp(-0.5) - Math.exp(-2.5));
  close(densityAt(preview, 3), expected);
});

previewTest('gamma shape and scale agree with exponential and Erlang densities', ['gamma'], () => {
  const gamma = continuous({ kind: 'gamma', unit: 'number', min: '1', max: '5', shape: '1', scale: '2' });
  const expected = (Math.exp(-1.5) / 2) / (Math.exp(-0.5) - Math.exp(-2.5));
  close(densityAt(gamma, 3), expected);
  const gammaTwo = continuous({ kind: 'gamma', unit: 'number', shape: '2', scale: '2', min: '0', max: '8' });
  close(densityAt(gammaTwo, 2), Math.exp(-1) / 2 / (1 - 5 * Math.exp(-4)));
});

previewTest('Weibull shape one agrees with the exponential density', ['weibull'], () => {
  const preview = continuous({ kind: 'weibull', unit: 'number', min: '1', max: '5', shape: '1', scale: '2' });
  const expected = (Math.exp(-1.5) / 2) / (Math.exp(-0.5) - Math.exp(-2.5));
  close(densityAt(preview, 3), expected);
});

previewTest('Student t location-scale conversion agrees with the independent Cauchy formula', ["studentT"], () => {
  const preview = continuous({ kind: 'studentT', unit: 'number', location: '2', scale: '1', degreesOfFreedom: '1', min: '0', max: '4' });
  close(preview.normalizationMass, 2 * Math.atan(2) / Math.PI);
  // jStat's Student-t PDF uses its beta-function approximation (about 1e-8).
  close(densityAt(preview, 2), 1 / (2 * Math.atan(2)), 1e-8);
});

previewTest('Beta affine support applies its Jacobian and keeps exact parameter labels', ["beta"], () => {
  const preview = continuous({ kind: 'beta', unit: 'nanoseconds', alpha: '2', beta: '3', min: '10', max: '20' });
  close(preview.normalizationMass, 1);
  close(densityAt(preview, 15), 0.15);
  assert.deepEqual(preview.parameters, [
    { name: 'alpha', value: '2' }, { name: 'beta', value: '3' }, { name: 'min', value: '10' }, { name: 'max', value: '20' },
  ]);
});

previewTest('lognormal median is converted to log-space mean', ["lognormal"], () => {
  const preview = continuous({ kind: 'lognormal', unit: 'number', median: '2', logStddev: '1', min: '0', max: '2' });
  close(preview.normalizationMass, 0.5);
  close(densityAt(preview, 2), 1 / Math.sqrt(2 * Math.PI));
});

previewTest('Pareto conditions its power-law density on fixed bounds', ['pareto'], () => {
  const profile = { kind: 'pareto', unit: 'bytes', scale: '1', shape: '2', min: '1', max: '5' } as const;
  const pareto = continuous(profile);
  close(densityAt(pareto, 2), 0.25 / (1 - 1 / 25));
  const lowerScale = continuous({ ...profile, scale: '0.5' });
  close(densityAt(lowerScale, 2), densityAt(pareto, 2));
});

previewTest('Weibull shape two matches its elementary conditional density', ['weibull'], () => {
  const weibull = continuous({ kind: 'weibull', unit: 'number', scale: '2', shape: '2', min: '0', max: '4' });
  close(densityAt(weibull, 2), Math.exp(-1) / (1 - Math.exp(-4)));
});

previewTest('triangular peak and endpoint modes remain analytical', ["triangular"], () => {
  const common = { kind: 'triangular', unit: 'number', min: '2', max: '8' } as const;
  const middle = continuous({ ...common, mode: '4' });
  close(densityAt(middle, 4), 1 / 3);
  close(densityAt(continuous({ ...common, mode: '2' }), 2), 1 / 3);
  close(densityAt(continuous({ ...common, mode: '8' }), 8), 1 / 3);
});

previewTest('Beta singular endpoints retain explicit omitted intervals', ['beta'], () => {
  const beta = continuous({ kind: 'beta', unit: 'number', alpha: '0.5', beta: '0.5', min: '2', max: '10' });
  assert.deepEqual(beta.singularities, [
    { edge: 'minimum', value: '2', finiteCurveBoundary: '2.015625' },
    { edge: 'maximum', value: '10', finiteCurveBoundary: '9.984375' },
  ]);
  assert.equal(beta.points[0].position, 1 / 512);
  assert.equal(beta.points[beta.points.length - 1].position, 511 / 512);
});

for (const kind of ['gamma', 'weibull'] as const) {
  previewTest(`${kind} singular minimum is explicit and its finite curve starts inside support`, [kind], () => {
    const preview = continuous({ kind, unit: 'number', shape: '0.5', scale: '1', min: '0', max: '4' });
    assert.deepEqual(preview.singularities, [
      { edge: 'minimum', value: '0', finiteCurveBoundary: '0.0078125' },
    ]);
    assert.ok(preview.points.every(point => Number.isFinite(point.density)));
    assert.ok(preview.points.every(point => point.position > 0));
  });
}

previewTest('Bernoulli handles endpoint probabilities and exact complementary fractions', ["bernoulli"], () => {
  for (const probability of ['0', '0.125', '1']) {
    const preview = buildDistributionPreview({ kind: 'bernoulli', unit: 'number', probability });
    assert.equal(preview.kind, 'pmf');
    if (preview.kind !== 'pmf') return;
    close(preview.bars.reduce((sum, bar) => sum + bar.probability, 0), 1);
    assert.deepEqual(preview.bars.map(bar => bar.value), ['0', '1']);
    if (probability === '0.125') assert.deepEqual(preview.bars.map(bar => bar.probabilityLabel), ['7 / 8', '1 / 8']);
  }
});

previewTest('Poisson conditions on integer [min,max), including the zero-rate point mass', ["poisson"], () => {
  const preview = buildDistributionPreview({ kind: 'poisson', unit: 'number', mean: '2', min: '1', max: '4' });
  assert.equal(preview.kind, 'pmf');
  if (preview.kind !== 'pmf') return;
  assert.deepEqual(preview.bars.map(bar => bar.value), ['1', '2', '3']);
  // Unnormalized masses after dropping exp(-2): 2, 2, 4/3.
  preview.bars.forEach((bar, index) => close(bar.probability, [3 / 8, 3 / 8, 1 / 4][index]));
  const zero = buildDistributionPreview({ kind: 'poisson', unit: 'number', mean: '0', min: '0', max: '3' });
  assert.equal(zero.kind, 'pmf');
  if (zero.kind === 'pmf') assert.deepEqual(zero.bars.map(bar => bar.probability), [1, 0, 0]);
});

previewTest('weighted sums, duplicate values and axes stay exact above Number safe integers', ["discreteWeighted"], () => {
  const profile: RandomProfile = {
    kind: 'discreteWeighted', unit: 'bytes', entries: [
      { value: '9007199254740992.375', weight: '9007199254740992' },
      { value: '9007199254740992.125', weight: '9007199254740992' },
      { value: '9007199254740992.125', weight: '1' },
      { value: '9007199254740992.25', weight: '0' },
    ],
  };
  const preview = buildDistributionPreview(profile);
  assert.equal(preview.kind, 'pmf');
  if (preview.kind !== 'pmf') return;
  assert.deepEqual(preview.bars.map(bar => bar.value), ['9007199254740992.125', '9007199254740992.25', '9007199254740992.375']);
  assert.deepEqual(preview.bars.map(bar => bar.probabilityLabel), ['9007199254740993 / 18014398509481985', '0', '9007199254740992 / 18014398509481985']);
  assert.equal(preview.axisMinimum, '9007199254740992.0625');
  assert.equal(preview.axisMaximum, '9007199254740992.4375');
  close(preview.bars[0].position, 1 / 6);
  close(preview.bars[2].position, 5 / 6);
  close(preview.bars.reduce((sum, bar) => sum + bar.probability, 0), 1);
});

previewTest('fractional weighted masses do not round away small positive weights', ["discreteWeighted"], () => {
  const preview = buildDistributionPreview({ kind: 'discreteWeighted', unit: 'number', entries: [
    { value: '1', weight: '0.0000000000000000000000001' }, { value: '2', weight: '1' },
  ] });
  assert.equal(preview.kind, 'pmf');
  if (preview.kind === 'pmf') {
    assert.equal(preview.bars[0].probabilityLabel, '1 / 10000000000000000000000001');
    assert.ok(preview.bars[0].probability > 0);
  }
});

previewTest('ECDF sorts exact values and groups equal-mass duplicates without interpolation', ["empirical"], () => {
  const preview = buildDistributionPreview({ kind: 'empirical', unit: 'bytes', observations: [
    '9007199254740992.375', '9007199254740992.125', '9007199254740992.125', '9007199254740992.25',
  ] });
  assert.equal(preview.kind, 'ecdf');
  if (preview.kind !== 'ecdf') return;
  assert.equal(preview.observationCount, 4);
  assert.deepEqual(preview.steps.map(({ value, before, after, count, cumulativeLabel }) => ({ value, before, after, count, cumulativeLabel })), [
    { value: '9007199254740992.125', before: 0, after: 0.5, count: 2, cumulativeLabel: '1 / 2' },
    { value: '9007199254740992.25', before: 0.5, after: 0.75, count: 1, cumulativeLabel: '3 / 4' },
    { value: '9007199254740992.375', before: 0.75, after: 1, count: 1, cumulativeLabel: '1' },
  ]);
  const singleton = buildDistributionPreview({ kind: 'empirical', unit: 'number', observations: ['2', '2'] });
  assert.equal(singleton.kind, 'ecdf');
  if (singleton.kind === 'ecdf') assert.equal(singleton.steps[0].position, 0.5);
});

test('exact validation precedes preview conversion and rejects malformed parameters', () => {
  const invalid = [
    { kind: 'normal', unit: 'number', mean: '1', stddev: '0', min: '0', max: '2' },
    { kind: 'bernoulli', unit: 'number', probability: '1.1' },
    { kind: 'beta', unit: 'number', alpha: '1', beta: '1', min: '1', max: '1' },
    { kind: 'discreteWeighted', unit: 'number', entries: [{ value: '1', weight: '0' }] },
    { kind: 'empirical', unit: 'number', observations: ['1e3'] },
  ];
  for (const profile of invalid.filter(profile => familyByKind(profile.kind))) {
    assert.throws(() => buildDistributionPreview(profile as RandomProfile));
  }
});

test('unsupported numerical axes, unresolved tail CDFs and excessive PMF ranges fail explicitly', () => {
  const invalid: RandomProfile[] = [
    { kind: 'normal', unit: 'bytes', mean: '9007199254740992', stddev: '1', min: '9007199254740992.125', max: '9007199254740992.375' },
    { kind: 'normal', unit: 'number', mean: '1', stddev: '1', min: '100', max: '101' },
    { kind: 'poisson', unit: 'number', mean: '2', min: '0', max: '513' },
    { kind: 'poisson', unit: 'number', mean: '1000001', min: '0', max: '2' },
    { kind: 'poisson', unit: 'number', mean: '1', min: '1000000', max: '1000001' },
    { kind: 'normal', unit: 'number', mean: '1', stddev: '0.0000000000000000000000001', min: '0', max: '2' },
    { kind: 'beta', unit: 'number', alpha: '0.9999999999999999999999999', beta: '2', min: '0', max: '2' },
    { kind: 'empirical', unit: 'number', observations: ['0', '0.0000000000000000000000001', '1'] },
  ];
  for (const profile of invalid.filter(profile => familyByKind(profile.kind))) {
    assert.throws(() => buildDistributionPreview(profile), /finite precision/);
  }
});

previewTest('repeated previews are deterministic and do not consume RNG or mutate exact inputs', ["normal"], () => {
  const profile: RandomProfile = { kind: 'normal', unit: 'number', mean: '2', stddev: '1', min: '0', max: '4' };
  const original = JSON.stringify(profile);
  const random = Math.random;
  let first: DistributionPreview;
  try {
    Math.random = () => { throw new Error('A documentation preview must not draw random values.'); };
    first = buildDistributionPreview(profile);
    assert.deepEqual(buildDistributionPreview(profile), first);
  } finally { Math.random = random; }
  assert.equal(JSON.stringify(profile), original);
});
