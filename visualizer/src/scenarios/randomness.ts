import { parseExactJSON } from './json.ts';
import { INT64_MAX } from './values.ts';

export type RandomUnit = 'nanoseconds' | 'bytes' | 'bitsPerSecond' | 'number' | 'percentage';

export type RandomProfile =
  | { readonly kind: 'constant'; readonly unit: RandomUnit; readonly value: string }
  | { readonly kind: 'uniform'; readonly unit: RandomUnit; readonly min: string; readonly max: string };

interface Quantity {
  text: string;
  numerator: bigint;
  denominator: bigint;
}

export function parseRandomProfile(document: string): RandomProfile {
  return validateRandomProfile(parseExactJSON(document));
}

// Return a fresh profile so callers cannot change it through the input object.
export function validateRandomProfile(input: unknown): RandomProfile {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new Error('Random profile must be an object.');
  }

  const profile = input as Record<string, unknown>;
  const kind = profile.kind;
  if (kind !== 'constant' && kind !== 'uniform') {
    throw new Error('Random profile kind must be constant or uniform.');
  }

  const fields = kind === 'constant' ? ['kind', 'unit', 'value'] : ['kind', 'unit', 'min', 'max'];
  const keys = Reflect.ownKeys(profile);
  if (keys.length !== fields.length || keys.some((key) => typeof key !== 'string' || !fields.includes(key))) {
    throw new Error(`Random ${kind} profile requires exactly ${fields.join(', ')}.`);
  }

  const unit = profile.unit;
  if (unit !== 'nanoseconds' && unit !== 'bytes' && unit !== 'bitsPerSecond'
    && unit !== 'number' && unit !== 'percentage') {
    throw new Error('Random profile has an unknown unit.');
  }

  if (kind === 'constant') {
    return { kind, unit, value: readQuantity(profile.value, unit, 'value').text };
  }

  const minimum = readQuantity(profile.min, unit, 'min');
  const maximum = readQuantity(profile.max, unit, 'max');
  // Uniform support is [min, max); comparing exact fractions preserves tiny gaps.
  if (minimum.numerator * maximum.denominator >= maximum.numerator * minimum.denominator) {
    throw new Error('Random uniform min must be less than max.');
  }
  return { kind, unit, min: minimum.text, max: maximum.text };
}

export function serializeRandomProfile(profile: RandomProfile): string {
  return JSON.stringify(validateRandomProfile(profile));
}

function readQuantity(value: unknown, unit: RandomUnit, field: string): Quantity {
  if (typeof value !== 'string' || value.length > 128) {
    throw new Error(`Random profile ${field} must be a decimal string of at most 128 characters.`);
  }

  const match = /^(?:0|[1-9][0-9]*)(?:\.[0-9]*[1-9])?$/.exec(value);
  if (!match || match[0] !== value) {
    throw new Error(`Random profile ${field} must be a canonical nonnegative decimal.`);
  }

  const point = value.indexOf('.');
  const numerator = BigInt(value.replace('.', ''));
  const denominator = point < 0 ? 1n : 10n ** BigInt(value.length - point - 1);
  const limit = unit === 'percentage' ? 100n : INT64_MAX;
  if (numerator > limit * denominator) {
    throw new Error(`Random profile ${field} must not exceed ${limit} ${unit}.`);
  }
  return { text: value, numerator, denominator };
}
