import type { ExactInteger } from '../scenarios/types.ts';
import { INT64_MAX } from '../scenarios/values.ts';
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

// Payload and window values count bytes, not bits or packets.
export function dataBytes(token: Token, minimum = 0n, maximum = INT64_MAX): ExactInteger {
  return integerQuantity(token, byteUnits, minimum, maximum);
}

// A link's 1GB means bytes/s; the unchanged native contract receives 8,000,000,000 bit/s.
export function bitRate(token: Token): ExactInteger {
  return integerQuantity(token, bitRateUnits, 1n, INT64_MAX);
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
