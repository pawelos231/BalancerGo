import jstat from 'jstat';
import { validateRandomProfile } from '../scenarios/randomness.ts';
import type { RandomProfile, RandomUnit } from '../scenarios/randomness.ts';

// jStat 1.9.6 parameter conventions: https://jstat.github.io/distributions.html
// Pinned implementation: https://github.com/jstat/jstat/tree/7c4d63ffdd9961f151aebc2ff78b3dc68c17143e
// This module evaluates definitions. It never samples or uses simulation results.
const { jStat } = jstat;
const unitLabels: Record<RandomUnit, string> = {
  nanoseconds: 'ns', bytes: 'B', bitsPerSecond: 'bit/s', number: 'unit', percentage: '%',
};

interface PreviewBase {
  readonly family: RandomProfile['kind'];
  readonly unitLabel: string;
  readonly axisMinimum: string;
  readonly axisMaximum: string;
  readonly parameters: readonly { name: string; value: string }[];
  readonly notes: readonly string[];
}

export interface DensityPoint { readonly position: number; readonly density: number }
export interface EndpointSingularity {
  readonly edge: 'minimum' | 'maximum';
  readonly value: string;
  readonly finiteCurveBoundary: string;
}

export interface ContinuousPreview extends PreviewBase {
  readonly kind: 'pdf';
  readonly minimum: string;
  readonly maximum: string;
  readonly normalizationMass: number;
  readonly densityMaximum: number;
  readonly points: readonly DensityPoint[];
  readonly singularities: readonly EndpointSingularity[];
}

export interface ProbabilityBar {
  readonly value: string;
  readonly position: number;
  readonly probability: number;
  readonly probabilityLabel: string;
}

export interface MassPreview extends PreviewBase {
  readonly kind: 'pmf';
  readonly bars: readonly ProbabilityBar[];
}

export interface EmpiricalStep {
  readonly value: string;
  readonly position: number;
  readonly before: number;
  readonly after: number;
  readonly count: number;
  readonly cumulativeLabel: string;
}

export interface EmpiricalPreview extends PreviewBase {
  readonly kind: 'ecdf';
  readonly observationCount: number;
  readonly steps: readonly EmpiricalStep[];
}

export type DistributionPreview = ContinuousPreview | MassPreview | EmpiricalPreview;

// Validate the exact decimal contract before any conversion for the drawing.
// The existing BigInt uniform/constant preview deliberately remains separate.
export function buildDistributionPreview(input: RandomProfile): DistributionPreview {
  const profile = validateRandomProfile(input);
  switch (profile.kind) {
    case 'bernoulli': return bernoulliPreview(profile);
    case 'poisson': return poissonPreview(profile);
    case 'discreteWeighted': return weightedPreview(profile);
    case 'empirical': return empiricalPreview(profile);
    case 'constant': case 'uniform':
      throw new Error('Constant and uniform profiles use their existing exact preview.');
    default: return continuousPreview(profile);
  }
}

type ContinuousProfile = Exclude<RandomProfile, {
  kind: 'constant' | 'uniform' | 'bernoulli' | 'poisson' | 'discreteWeighted' | 'empirical';
}>;

interface AnalyticLaw {
  pdf: (value: number) => number;
  cdf: (value: number) => number;
  landmarks: readonly number[];
  singularMinimum?: boolean;
  singularMaximum?: boolean;
}

function continuousPreview(profile: ContinuousProfile): ContinuousPreview {
  const minimum = numeric(profile.min, 'minimum');
  const maximum = numeric(profile.max, 'maximum');
  const width = maximum - minimum;
  if (!(width > 0) || width <= 64 * Number.EPSILON * Math.max(Math.abs(minimum), Math.abs(maximum))) {
    throw precisionError('The exact bounds cannot form a distinct numerical axis.');
  }

  const law = analyticLaw(profile, minimum, maximum);
  const lowerCDF = checkedCDF(law.cdf(minimum));
  const upperCDF = checkedCDF(law.cdf(maximum));
  const mass = upperCDF - lowerCDF;
  // Subtracting nearly equal CDF values would invent a poorly resolved curve.
  if (!(mass > 1e-12) || mass > 1 + 1e-12) {
    throw precisionError('The conditional probability within these bounds cannot be resolved reliably.');
  }

  const singularities: EndpointSingularity[] = [];
  const start = law.singularMinimum ? 1 / 512 : 0;
  const end = law.singularMaximum ? 511 / 512 : 1;
  if (law.singularMinimum) singularities.push({
    edge: 'minimum', value: profile.min,
    finiteCurveBoundary: decimalText(interpolate(profile.min, profile.max, 1n, 512n)),
  });
  if (law.singularMaximum) singularities.push({
    edge: 'maximum', value: profile.max,
    finiteCurveBoundary: decimalText(interpolate(profile.min, profile.max, 511n, 512n)),
  });

  // An analytical grid is independent of RNG state. Include modes and other
  // landmarks so, for example, a triangular peak is never flattened by the grid.
  const positions = new Set<number>([start, end]);
  for (let index = 1; index < 256; index += 1) {
    const position = index / 256;
    if (position > start && position < end) positions.add(position);
  }
  for (const value of law.landmarks) {
    const position = (value - minimum) / width;
    if (position >= start && position <= end) positions.add(position);
  }
  const points = [...positions].sort((first, second) => first - second).map(position => {
    const value = position === 1 ? maximum : minimum + position * width;
    const density = law.pdf(value) / mass;
    if (!Number.isFinite(density) || density < 0) {
      throw precisionError('A finite density cannot be evaluated on this axis.');
    }
    return { position, density };
  });
  // A very narrow peak can collapse between adjacent floating-point positions.
  // Refuse a misleading triangle when the fixed analytical grid cannot resolve it.
  const masses = points.map(point => (checkedCDF(law.cdf(minimum + point.position * width)) - lowerCDF) / mass);
  if (masses.some((value, index) => index > 0 && value - masses[index - 1] > 0.25)) {
    throw precisionError('The analytical curve is too concentrated for this chart resolution.');
  }
  const densityMaximum = Math.max(...points.map(point => point.density)) * 1.15;
  if (!(densityMaximum > 0) || !Number.isFinite(densityMaximum)) {
    throw precisionError('The density is too narrow or too small for this numerical axis.');
  }
  const notes = [
    profile.kind === 'beta'
      ? 'Beta is mapped affinely from [0, 1] to [min, max); density includes the reciprocal-width Jacobian.'
      : profile.kind === 'triangular'
        ? 'The full triangular support is [min, max); mode may equal either endpoint.'
        : 'The density is conditional on min ≤ X < max: f(x) / (F(max) − F(min)). Mass outside the bounds is excluded.',
    'The horizontal axis shows the complete configured support. The maximum is excluded; its curve value is a left-hand limit.',
    'This is an analytical numerical approximation before integer flooring or runtime conversion. No random samples are used.',
  ];
  if (singularities.length > 0) notes.push(
    'An endpoint arrow marks density tending to +∞. Each singular endpoint omits 1/512 of the horizontal span from the finite curve; no infinite height is replaced by a finite value.',
  );
  return {
    ...base(profile), kind: 'pdf', minimum: profile.min, maximum: profile.max,
    axisMinimum: profile.min, axisMaximum: profile.max,
    normalizationMass: mass, densityMaximum, points, singularities, notes,
  };
}

// Units belong to quantities; dimensionless shapes remain dimensionless.
function analyticLaw(profile: ContinuousProfile, minimum: number, maximum: number): AnalyticLaw {
  const parameter = (value: string, name: string) => numeric(value, name);
  switch (profile.kind) {
    case 'normal': {
      const mean = parameter(profile.mean, 'mean');
      const stddev = parameter(profile.stddev, 'stddev');
      return {
        pdf: x => jStat.normal.pdf(x, mean, stddev),
        cdf: x => jStat.normal.cdf(x, mean, stddev), landmarks: [mean],
      };
    }
    case 'exponential': {
      const mean = parameter(profile.mean, 'mean');
      return {
        pdf: x => jStat.exponential.pdf(x, 1 / mean),
        cdf: x => jStat.exponential.cdf(x, 1 / mean), landmarks: [],
      };
    }
    case 'gamma': {
      const shape = parameter(profile.shape, 'shape');
      const scale = parameter(profile.scale, 'scale');
      return {
        pdf: x => jStat.gamma.pdf(x, shape, scale),
        cdf: x => jStat.gamma.cdf(x, shape, scale),
        landmarks: [Math.max(0, shape - 1) * scale], singularMinimum: minimum === 0 && shape < 1,
      };
    }
    case 'studentT': {
      const location = parameter(profile.location, 'location');
      const scale = parameter(profile.scale, 'scale');
      const degrees = parameter(profile.degreesOfFreedom, 'degreesOfFreedom');
      return {
        pdf: x => jStat.studentt.pdf((x - location) / scale, degrees) / scale,
        cdf: x => jStat.studentt.cdf((x - location) / scale, degrees), landmarks: [location],
      };
    }
    case 'beta': {
      const alpha = parameter(profile.alpha, 'alpha');
      const beta = parameter(profile.beta, 'beta');
      const width = maximum - minimum;
      const mode = alpha > 1 && beta > 1 ? minimum + width * (alpha - 1) / (alpha + beta - 2) : minimum;
      return {
        pdf: x => jStat.beta.pdf((x - minimum) / width, alpha, beta) / width,
        cdf: x => x <= minimum ? 0 : x >= maximum ? 1 : jStat.beta.cdf((x - minimum) / width, alpha, beta),
        landmarks: [mode], singularMinimum: alpha < 1, singularMaximum: beta < 1,
      };
    }
    case 'lognormal': {
      const median = parameter(profile.median, 'median');
      const sigma = parameter(profile.logStddev, 'logStddev');
      const mu = Math.log(median);
      return {
        pdf: x => jStat.lognormal.pdf(x, mu, sigma),
        cdf: x => jStat.lognormal.cdf(x, mu, sigma), landmarks: [Math.exp(mu - sigma * sigma)],
      };
    }
    case 'pareto': {
      const scale = parameter(profile.scale, 'scale');
      const shape = parameter(profile.shape, 'shape');
      return {
        pdf: x => jStat.pareto.pdf(x, scale, shape),
        cdf: x => jStat.pareto.cdf(x, scale, shape), landmarks: [scale],
      };
    }
    case 'weibull': {
      const scale = parameter(profile.scale, 'scale');
      const shape = parameter(profile.shape, 'shape');
      return {
        pdf: x => jStat.weibull.pdf(x, scale, shape),
        cdf: x => jStat.weibull.cdf(x, scale, shape),
        landmarks: [shape > 1 ? scale * Math.pow((shape - 1) / shape, 1 / shape) : 0],
        singularMinimum: minimum === 0 && shape < 1,
      };
    }
    case 'triangular': {
      const mode = parameter(profile.mode, 'mode');
      return {
        pdf: x => jStat.triangular.pdf(x, minimum, maximum, mode),
        cdf: x => jStat.triangular.cdf(x, minimum, maximum, mode), landmarks: [mode],
      };
    }
  }
}

function bernoulliPreview(profile: Extract<RandomProfile, { kind: 'bernoulli' }>): MassPreview {
  const probability = fraction(profile.probability);
  const probabilities = [subtract({ numerator: 1n, denominator: 1n }, probability), probability];
  const axis = exactAxis(['0', '1']);
  return {
    ...base(profile), ...axis.labels, kind: 'pmf',
    bars: probabilities.map((mass, index) => ({
      value: String(index), position: axis.position(String(index)),
      probability: ratioNumber(mass), probabilityLabel: fractionLabel(mass),
    })),
    notes: ['Bars show exact probability mass at 0 and 1. Their heights are 1 − probability and probability; there is no density between outcomes.'],
  };
}

function poissonPreview(profile: Extract<RandomProfile, { kind: 'poisson' }>): MassPreview {
  const minimum = numeric(profile.min, 'minimum');
  const maximum = numeric(profile.max, 'maximum');
  const mean = numeric(profile.mean, 'mean');
  if (!Number.isSafeInteger(minimum) || !Number.isSafeInteger(maximum) || maximum - minimum > 512) {
    throw precisionError('The PMF preview supports at most 512 consecutive safe integer outcomes.');
  }
  if (maximum > 1_000_000 || mean > 1_000_000) {
    throw precisionError('Poisson parameters above 1,000,000 exceed this chart’s supported numerical range.');
  }
  const values = Array.from({ length: maximum - minimum }, (_, index) => minimum + index);
  // k log(lambda) - lambda - log(k!) is the Poisson PMF in log space.
  // Subtracting the largest log mass avoids factorial overflow and tiny-tail CDF cancellation.
  const logarithms = values.map(value => mean === 0
    ? value === 0 ? 0 : -Infinity
    : value * Math.log(mean) - mean - jStat.gammaln(value + 1));
  const largest = Math.max(...logarithms);
  const masses = logarithms.map(logarithm => Math.exp(logarithm - largest));
  if (mean > 0 && masses.some(mass => !Number.isFinite(mass) || mass === 0)) {
    throw precisionError('Poisson tail probabilities cannot be represented on this numerical chart.');
  }
  const total = masses.reduce((sum, value) => sum + value, 0);
  if (!(total > 0) || !Number.isFinite(total)) throw precisionError('The Poisson conditional mass cannot be resolved.');
  const axis = exactAxis(values.map(String));
  return {
    ...base(profile), ...axis.labels, kind: 'pmf',
    bars: values.map((value, index) => ({
      value: String(value), position: axis.position(String(value)), probability: masses[index] / total,
      probabilityLabel: `≈ ${(masses[index] / total).toPrecision(7)}`,
    })),
    notes: [
      `The bars are the Poisson PMF conditional on integer outcomes in [${profile.min}, ${profile.max}). The excluded maximum has no bar.`,
      'All configured outcomes are shown with padding. Numerical bar heights sum to 1; no random samples or histogram are used.',
    ],
  };
}

function weightedPreview(profile: Extract<RandomProfile, { kind: 'discreteWeighted' }>): MassPreview {
  const grouped = new Map<string, Fraction>();
  for (const entry of profile.entries) {
    grouped.set(entry.value, add(grouped.get(entry.value) ?? { numerator: 0n, denominator: 1n }, fraction(entry.weight)));
  }
  const values = [...grouped.keys()].sort(compareDecimal);
  const total = [...grouped.values()].reduce(add, { numerator: 0n, denominator: 1n });
  const axis = exactAxis(values);
  return {
    ...base(profile), ...axis.labels, kind: 'pmf',
    bars: values.map(value => {
      const mass = divide(grouped.get(value)!, total);
      return { value, position: axis.position(value), probability: ratioNumber(mass), probabilityLabel: fractionLabel(mass) };
    }),
    notes: [
      'Weights are summed exactly before normalization. Repeated values combine into one bar; a zero weight contributes zero mass.',
      'The complete set of values is shown with padding. Labels retain exact fractions; only normalized positions and heights use floating point.',
    ],
  };
}

function empiricalPreview(profile: Extract<RandomProfile, { kind: 'empirical' }>): EmpiricalPreview {
  const counts = new Map<string, number>();
  for (const value of profile.observations) counts.set(value, (counts.get(value) ?? 0) + 1);
  const values = [...counts.keys()].sort(compareDecimal);
  const axis = exactAxis(values);
  let cumulative = 0;
  const steps = values.map(value => {
    const before = cumulative;
    const count = counts.get(value)!;
    cumulative += count;
    return {
      value, position: axis.position(value), before: before / profile.observations.length,
      after: cumulative / profile.observations.length, count,
      cumulativeLabel: fractionLabel({ numerator: BigInt(cumulative), denominator: BigInt(profile.observations.length) }),
    };
  });
  return {
    ...base(profile), ...axis.labels, kind: 'ecdf', observationCount: profile.observations.length, steps,
    notes: [
      'Each observation has equal mass 1/n. Duplicate observations make a larger jump at the same exact value.',
      'The ECDF is right-continuous: an open marker shows the value before a jump and a filled marker includes that observation. Horizontal segments remain constant; there is no interpolation between observations.',
      'All observations are shown with padding. Values are sorted as exact decimals; no bins, smoothing or random samples are used.',
    ],
  };
}

function base(profile: RandomProfile): Omit<PreviewBase, 'axisMinimum' | 'axisMaximum' | 'notes'> {
  return {
    family: profile.kind, unitLabel: unitLabels[profile.unit],
    parameters: Object.entries(profile).flatMap(([name, value]) =>
      name === 'kind' || name === 'unit' || typeof value !== 'string' ? [] : [{ name, value }]),
  };
}

function numeric(value: string, name: string): number {
  const number = Number(value);
  if (!Number.isFinite(number) || number > Number.MAX_SAFE_INTEGER || (number === 0 && value !== '0')) {
    throw precisionError(`The exact ${name} value cannot be represented reliably on a numerical axis.`);
  }
  if (number === 1 && value !== '1') {
    throw precisionError(`The exact ${name} value is indistinguishable from the endpoint-shape boundary 1.`);
  }
  return number;
}

function checkedCDF(value: number): number {
  if (!Number.isFinite(value) || value < 0 || value > 1) throw precisionError('The analytical CDF cannot be evaluated reliably.');
  return value;
}

function precisionError(reason: string): Error {
  return new Error(`${reason} This profile remains exact, but this analytical preview does not support its finite precision.`);
}

interface Fraction { numerator: bigint; denominator: bigint }

function fraction(value: string): Fraction {
  const [whole, decimals = ''] = value.split('.');
  return { numerator: BigInt(whole + decimals), denominator: 10n ** BigInt(decimals.length) };
}

function add(first: Fraction, second: Fraction): Fraction {
  return reduce({ numerator: first.numerator * second.denominator + second.numerator * first.denominator,
    denominator: first.denominator * second.denominator });
}

function subtract(first: Fraction, second: Fraction): Fraction {
  return add(first, { numerator: -second.numerator, denominator: second.denominator });
}

function divide(first: Fraction, second: Fraction): Fraction {
  return reduce({ numerator: first.numerator * second.denominator, denominator: first.denominator * second.numerator });
}

function reduce(value: Fraction): Fraction {
  let first = value.numerator < 0n ? -value.numerator : value.numerator;
  let second = value.denominator;
  while (second !== 0n) [first, second] = [second, first % second];
  return { numerator: value.numerator / first, denominator: value.denominator / first };
}

function compareDecimal(first: string, second: string): number {
  const difference = subtract(fraction(first), fraction(second)).numerator;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function fractionLabel(value: Fraction): string {
  const reduced = reduce(value);
  return reduced.denominator === 1n ? String(reduced.numerator) : `${reduced.numerator} / ${reduced.denominator}`;
}

// Conversion happens after exact normalization, never on raw values or weights.
// Decimal long division also handles ratios whose BigInt operands exceed 1e308.
function ratioNumber(value: Fraction): number {
  const { numerator, denominator } = value;
  const whole = numerator / denominator;
  let remainder = numerator % denominator;
  let decimals = '';
  let significant = 0;
  while (remainder !== 0n && significant < 17 && decimals.length < 320) {
    remainder *= 10n;
    const digit = remainder / denominator;
    decimals += digit;
    remainder %= denominator;
    if (digit !== 0n || significant > 0) significant += 1;
  }
  const result = Number(`${whole}.${decimals || '0'}`);
  if (!Number.isFinite(result) || (numerator !== 0n && result === 0)) {
    throw precisionError('A normalized chart coordinate or probability is too small to represent.');
  }
  return result;
}

function interpolate(minimum: string, maximum: string, top: bigint, bottom: bigint): Fraction {
  const width = subtract(fraction(maximum), fraction(minimum));
  return add(fraction(minimum), { numerator: width.numerator * top, denominator: width.denominator * bottom });
}

// Padding uses exact arithmetic: [5, 15] receives the exact axis [2.5, 17.5].
function exactAxis(values: readonly string[]) {
  const first = fraction(values[0]);
  const last = fraction(values[values.length - 1]);
  const width = subtract(last, first);
  const padding = width.numerator === 0n ? { numerator: 1n, denominator: 1n }
    : { numerator: width.numerator, denominator: width.denominator * 4n };
  const lower = subtract(first, padding);
  const upper = add(last, padding);
  const span = subtract(upper, lower);
  const positions = new Map(values.map(value => [value, ratioNumber(divide(subtract(fraction(value), lower), span))]));
  if (values.some((value, index) => index > 0 && positions.get(value)! <= positions.get(values[index - 1])!)) {
    throw precisionError('Distinct exact values collapse to the same numerical chart position.');
  }
  return {
    labels: { axisMinimum: decimalText(lower), axisMaximum: decimalText(upper) },
    position: (value: string) => positions.get(value)!,
  };
}

// These denominators contain only 2 and 5, so labels terminate exactly.
function decimalText(value: Fraction): string {
  const negative = value.numerator < 0n;
  const absolute = negative ? -value.numerator : value.numerator;
  const whole = absolute / value.denominator;
  let remainder = absolute % value.denominator;
  let decimals = '';
  while (remainder !== 0n) {
    remainder *= 10n;
    decimals += remainder / value.denominator;
    remainder %= value.denominator;
  }
  return `${negative ? '-' : ''}${whole}${decimals ? `.${decimals}` : ''}`;
}
