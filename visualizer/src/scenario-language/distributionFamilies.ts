import { validateRandomProfile } from '../scenarios/randomness.ts';
import type { RandomProfile, RandomUnit, WeightedEntry } from '../scenarios/randomness.ts';
import { familyBySource } from '../scenarios/randomness/registry.ts';
import type { FamilySpec, ParameterSpec } from '../scenarios/randomness/types.ts';
import { decimalText, readBound } from './distributionValues.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { DistributionExpression, Property } from './syntax.ts';
import { decimalParts } from './values.ts';

const units = { time: 'nanoseconds', bytes: 'bytes', number: 'number', percentage: 'percentage' } as const;

// Parse exact parameters first. Recognition and runtime-field binding remain separate gates.
export function compileFamilyDefinition(expression: DistributionExpression): RandomProfile {
  const spec = familyBySource(expression.family.text);
  if (!spec || spec.kind === 'constant') {
    throw new LanguageError(`Unsupported distribution ${JSON.stringify(expression.family.text)}.`, expression.family.range);
  }
  let unit: RandomUnit | undefined = spec.numberOnly ? 'number' : undefined;
  const record: Record<string, unknown> = { kind: spec.kind };
  const seen = new Map<string, Property>();

  const quantity = (token: Token): string => {
    const bound = readBound(token);
    const nextUnit = units[bound.dimension];
    if (unit !== undefined && unit !== nextUnit) {
      throw new LanguageError(`${spec.title} quantity parameters must use compatible units.`, token.range);
    }
    unit = nextUnit;
    return decimalText(bound);
  };

  for (const property of expression.parameters) {
    const parameter = spec.parameters.find(item => (item.source ?? item.name) === property.key.text);
    if (!parameter) {
      throw new LanguageError(`Unknown ${spec.source} parameter ${JSON.stringify(property.key.text)}.`, property.key.range);
    }
    if (seen.has(parameter.name)) {
      throw new LanguageError(`Duplicate ${spec.source} parameter ${JSON.stringify(property.key.text)}.`, property.key.range);
    }
    seen.set(parameter.name, property);
    record[parameter.name] = readParameter(parameter, property, quantity);
  }
  for (const parameter of spec.parameters) {
    if (!seen.has(parameter.name)) {
      throw new LanguageError(`${spec.title} requires ${parameter.source ?? parameter.name}.`, expression.family.range);
    }
  }
  record.unit = unit ?? 'number';
  try {
    return validateRandomProfile(record);
  } catch (error) {
    let message = error instanceof Error ? error.message : 'Invalid distribution definition.';
    const parameter = /min.*max/.test(message) ? 'max'
      : spec.parameters.find(item => message.includes(item.name))?.name;
    const property = parameter ? seen.get(parameter) : undefined;
    const range = property && !Array.isArray(property.value) ? property.value.range : expression.range;
    for (const field of spec.parameters) {
      if (field.source) message = message.split(field.name).join(field.source);
    }
    throw new LanguageError(message, range);
  }
}

// Known parameter names provide diagnostics before this declaration can reach the native compiler.
export function requireFamilyCompilation(spec: FamilySpec, expression: DistributionExpression): void {
  if (!spec.compilationAvailable) {
    throw new LanguageError(
      `${spec.title} declaration is recognized; scenario compilation is not available yet.`,
      expression.family.range,
    );
  }
}

function readParameter(
  spec: ParameterSpec,
  property: Property,
  quantity: (token: Token) => string,
): string | WeightedEntry[] | string[] {
  if (spec.role === 'entries') return entries(property, quantity);
  if (spec.role === 'observations') return observations(property, quantity);
  const token = scalar(property);
  if (spec.role === 'quantity' || spec.role === 'positiveQuantity' || spec.role === 'integerQuantity') {
    return quantity(token);
  }
  const { numerator, denominator, unit } = decimalParts(token);
  if (spec.role === 'probability' && unit === '%') {
    return decimalText({ dimension: 'number', numerator, denominator: denominator * 100n });
  }
  if (unit !== '') throw new LanguageError(`Expected a dimensionless ${property.key.text}.`, token.range);
  return decimalText(readBound(token));
}

function scalar(property: Property): Token {
  if (Array.isArray(property.value) || property.value.kind === 'distribution') {
    throw new LanguageError(`Expected a numeric value for ${property.key.text}.`, property.key.range);
  }
  return property.value;
}

// Repeated entry blocks are intentional; fields inside each entry may appear only once.
function entries(property: Property, quantity: (token: Token) => string): WeightedEntry[] {
  if (!Array.isArray(property.value)) throw new LanguageError('Expected an entries block.', property.key.range);
  return property.value.map(item => {
    if (item.key.text !== 'entry' || !Array.isArray(item.value)) {
      throw new LanguageError('Entries contain entry { value ... weight ... } blocks.', item.key.range);
    }
    const fields = new Map<string, Property>();
    for (const field of item.value) {
      if (!['value', 'weight'].includes(field.key.text) || fields.has(field.key.text)) {
        throw new LanguageError('Each entry requires value and weight exactly once.', field.key.range);
      }
      fields.set(field.key.text, field);
    }
    if (!fields.has('value') || !fields.has('weight')) {
      throw new LanguageError('Each entry requires value and weight exactly once.', item.key.range);
    }
    const weight = scalar(fields.get('weight')!);
    const value = quantity(scalar(fields.get('value')!));
    if (decimalParts(weight).unit !== '') throw new LanguageError('Weights must be dimensionless.', weight.range);
    return { value, weight: decimalText(readBound(weight)) };
  });
}

// Duplicate observations retain their frequency in the empirical population.
function observations(property: Property, quantity: (token: Token) => string): string[] {
  if (!Array.isArray(property.value)) throw new LanguageError('Expected an observations block.', property.key.range);
  return property.value.map(item => {
    if (item.key.text !== 'observation') {
      throw new LanguageError('Observations contain observation values.', item.key.range);
    }
    return quantity(scalar(item));
  });
}
