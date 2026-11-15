import { validateRandomProfile } from '../scenarios/randomness.ts';
import type { RandomProfile, RandomUnit } from '../scenarios/randomness.ts';

interface AxisLabels {
  unitLabel: string;
  axisMinimum: string;
  axisMaximum: string;
}

// Values stay exact strings; the chart uses normalized positions, never rounded large bounds.
export type DensityPreview = AxisLabels & (
  | { kind: 'uniform'; minimum: string; maximum: string; density: string }
  | { kind: 'constant'; value: string }
);

const unitLabels: Record<RandomUnit, string> = {
  nanoseconds: 'ns', bytes: 'B', bitsPerSecond: 'bit/s', number: 'unit', percentage: '%',
};

// Build analytical labels only. No random source or future runtime sample is needed.
export function buildDensityPreview(input: RandomProfile): DensityPreview {
  const profile = validateRandomProfile(input);
  const unitLabel = unitLabels[profile.unit];
  if (profile.kind === 'constant') {
    const value = decimalFraction(profile.value);
    return {
      kind: 'constant', unitLabel, value: profile.value,
      axisMinimum: decimalText(value.numerator - value.scale, value.scale),
      axisMaximum: decimalText(value.numerator + value.scale, value.scale),
    };
  }

  const minimum = decimalFraction(profile.min);
  const maximum = decimalFraction(profile.max);
  const scale = minimum.scale > maximum.scale ? minimum.scale : maximum.scale;
  const lower = minimum.numerator * (scale / minimum.scale);
  const upper = maximum.numerator * (scale / maximum.scale);
  const width = upper - lower;

  // Pad by a quarter of the span: [5,15) is shown on an axis from 2.5 to 17.5.
  // The normalized support therefore begins at 1/6 and ends at 5/6 of the axis.
  return {
    kind: 'uniform', unitLabel, minimum: profile.min, maximum: profile.max,
    axisMinimum: decimalText(4n * lower - width, 4n * scale),
    axisMaximum: decimalText(4n * upper + width, 4n * scale),
    density: fractionText(scale, width),
  };
}

// For 0.125, retain 125/1000 instead of passing through Number.
function decimalFraction(value: string): { numerator: bigint; scale: bigint } {
  const [whole, fraction = ''] = value.split('.');
  return { numerator: BigInt(whole + fraction), scale: 10n ** BigInt(fraction.length) };
}

// Axis denominators contain only factors of 2 and 5, so their decimals terminate.
function decimalText(numerator: bigint, denominator: bigint): string {
  const sign = numerator < 0n ? '-' : '';
  const absolute = numerator < 0n ? -numerator : numerator;
  const whole = absolute / denominator;
  let remainder = absolute % denominator;
  if (remainder === 0n) return `${sign}${whole}`;

  let fraction = '';
  while (remainder !== 0n) {
    remainder *= 10n;
    fraction += (remainder / denominator).toString();
    remainder %= denominator;
  }
  return `${sign}${whole}.${fraction}`;
}

// Keep repeating densities exact: a span of 3 has density 1/3, not a rounded decimal.
function fractionText(numerator: bigint, denominator: bigint): string {
  let first = numerator;
  let second = denominator;
  while (second !== 0n) {
    [first, second] = [second, first % second];
  }
  const top = numerator / first;
  const bottom = denominator / first;
  return bottom === 1n ? top.toString() : `${top} / ${bottom}`;
}
