import { parseExactJSON } from '../json.ts';
import { INT64_MAX } from '../values.ts';
import { familyByKind } from './registry.ts';
import type { ParameterRole, RandomProfile, RandomUnit } from './types.ts';

interface Quantity { text: string; numerator: bigint; denominator: bigint }

export function parseRandomProfile(document: string): RandomProfile {
  return validateRandomProfile(parseExactJSON(document));
}

// Return a detached definition after checking every parameter; this never draws a value.
export function validateRandomProfile(input: unknown): RandomProfile {
  const profile = readObject(input, 'Random profile');
  const family = familyByKind(profile.kind);
  if (!family) {
    throw new Error('Random profile has an unsupported kind.');
  }
  checkFields(profile, ['kind', 'unit', ...family.parameters.map(parameter => parameter.name)], family.title);
  const unit = readUnit(profile.unit);
  if (family.numberOnly && unit !== 'number') {
    throw new Error(`Random ${family.source} requires number units.`);
  }

  const result: Record<string, unknown> = { kind: family.kind, unit };
  const quantities = new Map<string, Quantity>();
  for (const parameter of family.parameters) {
    const value = profile[parameter.name];
    if (parameter.role === 'entries') {
      result.entries = readEntries(value, unit);
    } else if (parameter.role === 'observations') {
      result.observations = readObservations(value, unit);
    } else {
      const quantity = readParameter(value, unit, parameter.role, parameter.name);
      result[parameter.name] = quantity.text;
      quantities.set(parameter.name, quantity);
    }
  }
  if (family.bounded && compare(quantities.get('min')!, quantities.get('max')!) >= 0) {
    throw new Error(`Random ${family.source} min must be less than max.`);
  }
  if (family.kind === 'triangular') {
    const mode = quantities.get('mode')!;
    if (compare(mode, quantities.get('min')!) < 0 || compare(mode, quantities.get('max')!) > 0) {
      throw new Error('Random triangular mode must be between min and max.');
    }
  }
  if (family.kind === 'pareto' && compare(quantities.get('min')!, quantities.get('scale')!) < 0) {
    throw new Error('Random pareto min must be at least scale.');
  }
  if (family.kind === 'poisson' && quantities.get('mean')!.numerator === 0n
    && quantities.get('min')!.numerator !== 0n) {
    throw new Error('Random poisson with mean zero must include zero in its support.');
  }
  return result as unknown as RandomProfile;
}

export function serializeRandomProfile(profile: RandomProfile): string {
  return JSON.stringify(validateRandomProfile(profile));
}

function readObject(input: unknown, path: string): Record<string, unknown> {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new Error(`${path} must be an object.`);
  }
  return input as Record<string, unknown>;
}

function checkFields(value: Record<string, unknown>, fields: readonly string[], label: string): void {
  const keys = Reflect.ownKeys(value);
  if (keys.length !== fields.length || keys.some(key => typeof key !== 'string' || !fields.includes(key))) {
    throw new Error(`Random ${label.toLowerCase()} profile requires exactly ${fields.join(', ')}.`);
  }
}

function readUnit(value: unknown): RandomUnit {
  if (value !== 'nanoseconds' && value !== 'bytes' && value !== 'bitsPerSecond'
    && value !== 'number' && value !== 'percentage') {
    throw new Error('Random profile has an unknown unit.');
  }
  return value;
}

// Dimensionless shapes do not inherit the percentage unit's ceiling of 100.
function readParameter(value: unknown, unit: RandomUnit, role: ParameterRole, field: string): Quantity {
  const dimensional = role === 'quantity' || role === 'positiveQuantity' || role === 'integerQuantity';
  const quantity = readQuantity(value, dimensional ? unit : 'number', field);
  if ((role === 'positiveQuantity' || role === 'shape') && quantity.numerator === 0n) {
    throw new Error(`Random profile ${field} must be positive.`);
  }
  if (role === 'integerQuantity' && quantity.denominator !== 1n) {
    throw new Error(`Random profile ${field} must be an integer.`);
  }
  if (role === 'probability' && quantity.numerator > quantity.denominator) {
    throw new Error(`Random profile ${field} must be between 0 and 1.`);
  }
  return quantity;
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

function readEntries(input: unknown, unit: RandomUnit): { value: string; weight: string }[] {
  if (!Array.isArray(input) || input.length === 0 || input.length > 4096) {
    throw new Error('Random weighted entries must contain 1 to 4096 items.');
  }
  let hasPositiveWeight = false;
  const entries = Array.from(input, (item, index) => {
    const entry = readObject(item, `Random weighted entry ${index}`);
    checkFields(entry, ['value', 'weight'], 'weighted entry');
    const value = readQuantity(entry.value, unit, 'value');
    const weight = readQuantity(entry.weight, 'number', 'weight');
    hasPositiveWeight ||= weight.numerator > 0n;
    return { value: value.text, weight: weight.text };
  });
  if (!hasPositiveWeight) {
    throw new Error('Random weighted entries require a positive total weight.');
  }
  return entries;
}

function readObservations(input: unknown, unit: RandomUnit): string[] {
  if (!Array.isArray(input) || input.length === 0 || input.length > 4096) {
    throw new Error('Random empirical observations must contain 1 to 4096 items.');
  }
  return Array.from(input, value => readQuantity(value, unit, 'observation').text);
}

function compare(first: Quantity, second: Quantity): number {
  const difference = first.numerator * second.denominator - second.numerator * first.denominator;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}
