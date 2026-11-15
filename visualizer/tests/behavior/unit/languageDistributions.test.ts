import assert from 'node:assert/strict';
import test from 'node:test';
import { validateUniformDistribution } from '../../../src/scenario-language/distributions.ts';
import { FieldWriter } from '../../../src/scenario-language/fields.ts';
import { parse } from '../../../src/scenario-language/parser.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';
import type { DistributionExpression } from '../../../src/scenario-language/syntax.ts';
import { timeValue } from '../../../src/scenario-language/values.ts';

function uniform(parameters: string): DistributionExpression {
  const program = parse(`defaults travel uniform { ${parameters} }`);
  const expression = program.declarations[0].properties[0].value;
  assert.ok(!Array.isArray(expression) && expression.kind === 'distribution');
  return expression;
}

// 500us and 1ms describe compatible time bounds; the original units stay in the AST.
test('uniform declarations compare compatible quantities without rewriting the source', () => {
  const expression = uniform('min 500us max 1ms');
  validateUniformDistribution(expression);

  assert.deepEqual(expression.parameters.map(parameter => {
    assert.ok(!Array.isArray(parameter.value) && 'text' in parameter.value);
    return parameter.value.text;
  }), ['500us', '1ms']);

  for (const parameters of [
    'max 1MB min 500KB',
    'min 1% max 5%',
    'min 0 max 10',
    'min 0.125B max 1B',
  ]) {
    assert.doesNotThrow(() => validateUniformDistribution(uniform(parameters)), parameters);
  }
});

// Adjacent large bounds and tiny decimal differences must not collapse to one float.
test('uniform bound comparison preserves large integers and decimal precision', () => {
  const valid = [
    'min 9007199254740992ns max 9007199254740993ns',
    'min 1.0000000000000000001 max 1.0000000000000000002',
    'min 99.99999999999999999% max 100%',
  ];
  for (const parameters of valid) {
    assert.doesNotThrow(() => validateUniformDistribution(uniform(parameters)), parameters);
  }
});

// Bounds need two distinct endpoints: a fixed 1ms cost still uses packet-time 1ms.
test('uniform rejects empty, equal and reversed ranges', () => {
  for (const parameters of ['', 'min 1ms', 'max 2ms']) {
    assert.throws(() => validateUniformDistribution(uniform(parameters)), /requires both min and max/);
  }

  for (const parameters of [
    'min 1ms max 500us',
    'min 1ms max 1000us',
    'min 1KB max 1000B',
    'min 0 max 0',
  ]) {
    assert.throws(() => validateUniformDistribution(uniform(parameters)), /min must be less than max/);
  }
});

// Parameter mistakes point to the supplied field instead of being treated as defaults.
test('uniform rejects unknown, duplicate and nonnumeric parameters', () => {
  const cases = [
    ['min 1ms max 2ms mean 1ms', /Unknown uniform parameter "mean"/],
    ['min 1ms min 2ms max 3ms', /Duplicate uniform parameter "min"/],
    ['min "1ms" max 2ms', /Expected a numeric value/],
    ['min null max 2ms', /Expected a numeric value/],
    ['min {} max 2ms', /Expected a numeric bound for min/],
    ['min uniform { min 0 max 1 } max 2ms', /Expected a numeric bound for min/],
  ] as const;

  for (const [parameters, message] of cases) {
    assert.throws(() => validateUniformDistribution(uniform(parameters)), message);
  }
});

// A time cannot be paired with bytes, and probabilities cannot exceed 100%.
test('uniform rejects incompatible units and out-of-range quantities', () => {
  const cases = [
    ['min 1ms max 2KB', /compatible units/],
    ['min 1 max 2%', /compatible units/],
    ['min 1Mbps max 2Mbps', /Unsupported uniform bound unit/],
    ['min -1ms max 2ms', /bounds must be between/],
    ['min 0% max 100.00000000000000001%', /bounds must be between/],
    ['min 0ns max 9223372036854775808ns', /bounds must be between/],
  ] as const;

  for (const [parameters, message] of cases) {
    assert.throws(() => validateUniformDistribution(uniform(parameters)), message);
  }
});

// Direct field writers cannot accidentally treat uniform parameters as an ordinary block.
test('field writing refuses a checked distribution until native compilation is supported', () => {
  const properties = parse('defaults travel uniform { min 1ms max 2ms }').declarations[0].properties;
  const target = {};

  assert.throws(
    () => new FieldWriter().apply(properties, { travel: { path: 'travel', read: timeValue } }, target, ''),
    error => {
      assert.ok(error instanceof LanguageError);
      assert.match(error.message, /syntax is recognized, but scenario compilation is not available yet/);
      assert.equal(error.range.start.column, 17);
      return true;
    },
  );
  assert.deepEqual(target, {});
});
