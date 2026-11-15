import { INT64_MAX } from '../scenarios/values.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { DistributionExpression, Program, Property } from './syntax.ts';
import { byteUnits, decimalParts, timeUnits } from './values.ts';

interface Bound {
  dimension: 'number' | 'time' | 'bytes' | 'percentage';
  numerator: bigint;
  denominator: bigint;
}

// Check the declared [min, max) range, for example 500us <= x < 1ms.
// This reads parameters only; sampling and native JSON compilation come later.
export function validateUniformDistribution(expression: DistributionExpression): void {
  if (expression.family.text !== 'uniform') {
    throw new LanguageError('Only the uniform distribution is supported here.', expression.family.range);
  }

  const parameters = new Map<string, Token>();
  for (const property of expression.parameters) {
    const name = property.key.text;
    if (name !== 'min' && name !== 'max') {
      throw new LanguageError(`Unknown uniform parameter ${JSON.stringify(name)}.`, property.key.range);
    }
    if (parameters.has(name)) {
      throw new LanguageError(`Duplicate uniform parameter ${JSON.stringify(name)}.`, property.key.range);
    }

    const value = property.value;
    if (Array.isArray(value) || value.kind === 'distribution') {
      throw new LanguageError(`Expected a numeric bound for ${name}.`, property.key.range);
    }
    parameters.set(name, value);
  }

  const minimum = parameters.get('min');
  const maximum = parameters.get('max');
  if (!minimum || !maximum) {
    throw new LanguageError('Uniform requires both min and max bounds.', expression.family.range);
  }

  const lower = readBound(minimum);
  const upper = readBound(maximum);
  if (lower.dimension !== upper.dimension) {
    throw new LanguageError('Uniform min and max must use compatible units.', maximum.range);
  }

  // Compare exact fractions: 0.5ms equals 500us, even above JavaScript's safe integers.
  const scaledMinimum = lower.numerator * upper.denominator;
  const scaledMaximum = upper.numerator * lower.denominator;
  if (scaledMinimum >= scaledMaximum) {
    throw new LanguageError('Uniform min must be less than max; use a scalar for a fixed value.', maximum.range);
  }
}

// Keep drafts explicit until E5.1.3 connects distributions to the native scenario.
export function rejectUncompiledDistribution(expression: DistributionExpression): never {
  validateUniformDistribution(expression);
  throw new LanguageError(
    'Uniform distribution syntax is recognized, but scenario compilation is not available yet.',
    expression.family.range,
  );
}

// Include unused profiles and timeline additions, so no declaration is silently discarded.
export function rejectUncompiledDistributions(program: Program): void {
  const visit = (properties: Property[]) => {
    for (const property of properties) {
      if (Array.isArray(property.value)) visit(property.value);
      else if (property.value.kind === 'distribution') rejectUncompiledDistribution(property.value);
    }
  };

  for (const declaration of program.declarations) visit(declaration.properties);
  for (const block of program.timeline) {
    for (const command of block.commands) {
      if (command.kind === 'set') visit(command.properties);
      else if (command.kind === 'add') visit(command.declaration.properties);
    }
  }
}

// Convert units for comparison without rounding bounds to a sampled integer value.
function readBound(token: Token): Bound {
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
