import { decimalText, readBound, type Bound } from './distributionValues.ts';
import { validateRandomProfile } from '../scenarios/randomness.ts';
import type { RandomProfile, RandomUnit } from '../scenarios/randomness.ts';
import { prefixName } from './identity.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { Declaration, DistributionExpression, Program, Property } from './syntax.ts';
import type { SourceRange } from './source.ts';
import { familyBySource } from '../scenarios/randomness/registry.ts';
import { compileFamilyDefinition, requireFamilyCompilation } from './distributionFamilies.ts';


// Check the declared [min, max) range, for example 500us <= x < 1ms.
// This reads parameters only; no random values are drawn during compilation.
export function validateUniformDistribution(expression: DistributionExpression): void {
  checkedUniformBounds(expression);
}

// Validate once and return exact bounds for both compilation and declaration checks.
function checkedUniformBounds(expression: DistributionExpression): { minimum: Bound; maximum: Bound } {
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
  return { minimum: lower, maximum: upper };
}

// Named definitions are stored, but binding one to a runtime field is a later stage.
export function rejectInlineDistribution(expression: DistributionExpression): never {
  if (expression.family.text === 'uniform') validateUniformDistribution(expression);
  else compileFamilyDefinition(expression);
  throw new LanguageError(
    `${familyBySource(expression.family.text)?.title ?? 'Distribution'} definitions compile as named distributions; runtime field sampling is not available yet.`,
    expression.family.range,
  );
}

// Include unused profiles and timeline additions, so no declaration is silently discarded.
export function rejectInlineDistributions(program: Program): void {
  const visit = (properties: Property[]) => {
    for (const property of properties) {
      if (Array.isArray(property.value)) visit(property.value);
      else if (property.value.kind === 'distribution') rejectInlineDistribution(property.value);
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

// Store unbound definitions in fixed.randomProfiles, for example 5us becomes "5000" ns.
// Names and source locations are retained; runtime fields keep their scalar contract.
export function compileRandomProfiles(
  declarations: Declaration[],
  locations: Map<string, SourceRange>,
): Record<string, RandomProfile> | undefined {
  const result: Record<string, RandomProfile> = Object.create(null);
  for (const declaration of declarations.filter(item => item.keyword.text === 'distribution')) {
    const id = prefixName(declaration.argument!);
    if (Object.prototype.hasOwnProperty.call(result, id)) {
      throw new LanguageError(`Duplicate distribution ${id}.`, declaration.argument!.range);
    }
    const expression = declaration.distribution!;
    const path = `fixed.randomProfiles[${JSON.stringify(id)}]`;
    locations.set(path, expression.range);
    for (const parameter of expression.parameters) {
      locations.set(`${path}.${parameter.key.text}`, parameter.key.range);
    }
    if (expression.family.text === 'uniform') result[id] = compileUniformDistribution(expression);
    else {
      result[id] = compileFamilyDefinition(expression);
      requireFamilyCompilation(familyBySource(expression.family.text)!, expression);
    }
  }
  return Object.keys(result).length ? result : undefined;
}

// Convert exact source bounds to canonical base-unit decimals, without rounding or sampling.
export function compileUniformDistribution(expression: DistributionExpression): RandomProfile {
  const { minimum, maximum } = checkedUniformBounds(expression);
  const units: Record<Bound['dimension'], RandomUnit> = {
    time: 'nanoseconds', bytes: 'bytes', number: 'number', percentage: 'percentage',
  };
  try {
    return validateRandomProfile({
      kind: 'uniform', unit: units[minimum.dimension],
      min: decimalText(minimum), max: decimalText(maximum),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Invalid uniform definition.';
    throw new LanguageError(message, expression.range);
  }
}

