import { INT64_MAX } from '../scenarios/values.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import { byteUnits, decimalParts, timeUnits } from './values.ts';

export interface Bound {
  dimension: 'number' | 'time' | 'bytes' | 'percentage';
  numerator: bigint;
  denominator: bigint;
}

// A power-of-ten denominator keeps 0.125 exact, and removes redundant trailing zeros.
export function decimalText(bound: Bound): string {
  const whole = bound.numerator / bound.denominator;
  const remainder = bound.numerator % bound.denominator;
  if (remainder === 0n) return whole.toString();
  const places = bound.denominator.toString().length - 1;
  const fraction = remainder.toString().padStart(places, '0').replace(/0+$/, '');
  return `${whole}.${fraction}`;
}

// Convert units for comparison without rounding bounds to a sampled integer value.
export function readBound(token: Token): Bound {
  const { numerator, denominator, unit } = decimalParts(token);
  let dimension: Bound['dimension'];
  let multiplier = 1n;

  if (Object.prototype.hasOwnProperty.call(timeUnits, unit)) {
    dimension = 'time';
    multiplier = timeUnits[unit];
  } else if (Object.prototype.hasOwnProperty.call(byteUnits, unit)) {
    dimension = 'bytes';
    multiplier = byteUnits[unit];
  } else if (unit === '%') dimension = 'percentage';
  else if (unit === '') dimension = 'number';
  else throw new LanguageError(`Unsupported uniform bound unit ${JSON.stringify(unit)}.`, token.range);

  const value = numerator * multiplier;
  const limit = dimension === 'percentage' ? 100n : INT64_MAX;
  if (value < 0n || value > limit * denominator) {
    throw new LanguageError(`Uniform bounds must be between 0 and ${limit} in their base unit.`, token.range);
  }
  return { dimension, numerator: value, denominator };
}
