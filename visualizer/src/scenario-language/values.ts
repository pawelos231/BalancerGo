import type { ExactInteger } from '../scenarios/types.ts';
import { INT64_MAX, INT64_MIN, UINT64_MAX } from '../scenarios/values.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';

const byteUnits: Readonly<Record<string, bigint>> = {
  B: 1n,
  KB: 1000n,
  MB: 1000n ** 2n,
  GB: 1000n ** 3n,
  TB: 1000n ** 4n,
  PB: 1000n ** 5n,
  EB: 1000n ** 6n,
};
const bitRateUnits = Object.fromEntries(Object.entries(byteUnits).map(([unit, bytes]) => [unit, bytes * 8n]));
const timeUnits = { ns: 1n, us: 1000n, ms: 1_000_000n, s: 1_000_000_000n, m: 60_000_000_000n };

// Payload and window values count bytes, not bits or packets.
export function dataBytes(token: Token, minimum = 0n, maximum = INT64_MAX): ExactInteger {
  return integerQuantity(token, byteUnits, minimum, maximum);
}

// A link's 1GB means bytes/s; the unchanged native contract receives 8,000,000,000 bit/s.
export function bitRate(token: Token): ExactInteger {
  return integerQuantity(token, bitRateUnits, 1n, INT64_MAX);
}

// 0.5ms becomes exactly 500,000ns; fractions of one nanosecond are rejected.
export function timeValue(token: Token, minimum = 0n): ExactInteger {
  return integerQuantity(token, timeUnits, minimum, INT64_MAX);
}

export function countValue(token: Token, minimum = 0n, maximum = INT64_MAX): ExactInteger {
  return integerQuantity(token, { '': 1n }, minimum, maximum);
}

// Seeds are signed int64, while the event budget can use the full uint64 range.
export function seedValue(token: Token): ExactInteger {
  return countValue(token, INT64_MIN, INT64_MAX);
}

export function eventLimit(token: Token): ExactInteger {
  return countValue(token, 1n, UINT64_MAX);
}

// Playback speed is an intentional float; virtual timestamps remain exact integers.
export function speedValue(token: Token): number {
  const { unit } = decimalParts(token);
  const value = Number(token.text);
  if (unit !== '' || !Number.isFinite(value) || value < 0) {
    throw new LanguageError('Speed must be a finite nonnegative value without a unit.', token.range);
  }
  return value;
}

// Thresholds retain percent units: 80% means an occupancy threshold of 80.
export function percentage(token: Token): number {
  const { numerator, denominator, unit } = decimalParts(token);
  if (unit !== '%' || numerator < 0n || numerator > 100n * denominator) {
    throw new LanguageError('Expected a percentage between 0% and 100%.', token.range);
  }
  return Number(token.text.slice(0, -1));
}

// Native probabilities use fractions: a 1% loss rate becomes 0.01.
export function probability(token: Token): number {
  return percentage(token) / 100;
}

export function booleanValue(token: Token): boolean {
  if (token.kind !== 'word' || !['true', 'false'].includes(token.text)) {
    throw new LanguageError('Expected true or false.', token.range);
  }
  return token.text === 'true';
}

export function textValue(token: Token): string {
  if (token.kind !== 'string' || token.value === undefined) {
    throw new LanguageError('Expected a double-quoted text value.', token.range);
  }
  return token.value;
}

// Keep decimal digits exact until units are applied: 0.001KB is exactly one byte.
export function decimalParts(token: Token): { numerator: bigint; denominator: bigint; unit: string } {
  if (token.kind !== 'number') throw new LanguageError('Expected a numeric value.', token.range);
  if (token.text.length > 128) {
    throw new LanguageError('A numeric literal may contain at most 128 characters.', token.range);
  }

  const match = /^(-?)([0-9]+)(?:\.([0-9]+))?([A-Za-z]+|%)?$/.exec(token.text);
  if (!match) throw new LanguageError('Invalid decimal value.', token.range);
  const fraction = match[3] ?? '';
  const sign = match[1] === '-' ? -1n : 1n;
  return {
    numerator: sign * BigInt(match[2] + fraction),
    denominator: 10n ** BigInt(fraction.length),
    unit: match[4] ?? '',
  };
}

// Divide only when exact; 0.1B is invalid for bytes, whereas 0.125B/s is one bit/s.
export function integerQuantity(
  token: Token,
  units: Readonly<Record<string, bigint>>,
  minimum: bigint,
  maximum: bigint,
): ExactInteger {
  const { numerator, denominator, unit } = decimalParts(token);
  if (!Object.prototype.hasOwnProperty.call(units, unit)) {
    throw new LanguageError(`Unsupported unit ${JSON.stringify(unit)} for this field.`, token.range);
  }

  const scaled = numerator * units[unit];
  if (scaled % denominator !== 0n) {
    throw new LanguageError('Value does not resolve to a whole native unit.', token.range);
  }
  const value = scaled / denominator;
  if (value < minimum || value > maximum) {
    throw new LanguageError(`Value must be between ${minimum} and ${maximum}.`, token.range);
  }
  return exactInteger(value);
}

// JSON has no bigint; unsafe integers remain decimal strings for Go and file export.
export function exactInteger(value: bigint): ExactInteger {
  const safe = BigInt(Number.MAX_SAFE_INTEGER);
  return value >= -safe && value <= safe ? Number(value) : value.toString();
}
