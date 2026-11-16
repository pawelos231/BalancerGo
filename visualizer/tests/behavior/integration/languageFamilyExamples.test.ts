import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { compileFamilyDefinition } from '../../../src/scenario-language/distributionFamilies.ts';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import { parse } from '../../../src/scenario-language/parser.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';
import { familySpecs } from '../../../src/scenarios/randomness/registry.ts';
import { sourceExamples } from '../../../src/scenarios/sourceExamples.ts';

test('family quantity diagnostics describe distribution units and ranges', () => {
  const definition = (mean: string) => parse(`distribution cost normal {
    mean ${mean} stddev 1us min 0us max 2us
  }`).declarations[0].distribution!;
  assert.throws(() => compileFamilyDefinition(definition('1Mbps')), /Unsupported distribution unit "Mbps"/);
  assert.throws(() => compileFamilyDefinition(definition('-1us')), /Distribution values must be between 0/);
});

test('family relation diagnostics highlight the offending parameter value', () => {
  const cases = [
    ['triangular { min 1ms mode 6ms max 5ms }', '6ms'],
    ['pareto { scale 2ms shape 1 min 1ms max 5ms }', '1ms'],
    ['normal { mean 1ms stddev 1ms min 3ms max 2ms }', '2ms'],
  ] as const;

  for (const [expression, value] of cases) {
    const source = `distribution invalid ${expression}`;
    const definition = parse(source).declarations[0].distribution!;
    assert.throws(() => compileFamilyDefinition(definition), error => {
      assert.ok(error instanceof LanguageError);
      assert.equal(source.slice(error.range.start.offset, error.range.end.offset), value);
      return true;
    });
  }
});

for (const family of familySpecs.filter(item => !['constant', 'uniform'].includes(item.kind))) {
  const examples = sourceExamples.filter(item => item.id.startsWith(`${family.source}-`));
  const valid = examples.find(item => item.id.endsWith('-profile-definitions'))!;
  const fixture = JSON.parse(readFileSync(new URL(
    `../../../../examples/randomness/families/${family.source}-cases.json`, import.meta.url,
  ), 'utf8'));

  // Library entries and their diagnostics use the actual canonical source files.
  test(`${family.source}: catalog sources and expected diagnostics agree`, () => {
    assert.equal(examples.length, 3);
    for (const example of examples) {
      const sourceFile = new URL(`../../../src/scenarios/examples/sources/${example.id}.lb`, import.meta.url);
      assert.equal(example.source, readFileSync(sourceFile, 'utf8'));
      const diagnostic = checkLanguageSource(example.source);
      if (example.expectedDiagnostic) {
        assert.ok(diagnostic);
        assert.ok(diagnostic.message.endsWith(`: ${example.expectedDiagnostic}`));
      } else {
        assert.equal(diagnostic, null);
      }
    }
  });

  // Exact definitions survive compilation; enabling it never changes scalar runtime settings.
  test(`${family.source}: exact native definitions and compilation gate agree`, () => {
    const definitions = Object.fromEntries(parse(valid.source).declarations
      .filter(item => item.distribution)
      .map(item => [item.argument!.text, compileFamilyDefinition(item.distribution!)]));
    assert.deepEqual(definitions, fixture.expectedProfiles);
    if (!family.compilationAvailable) {
      assert.throws(() => compileScenario(valid.source), /scenario compilation is not available yet/);
      return;
    }
    const compiled = compileScenario(valid.source);
    assert.deepEqual({ ...compiled.fixed.randomProfiles }, fixture.expectedProfiles);
    const native = JSON.parse(readFileSync(new URL(
      `../../../../examples/randomness/families/${family.source}-scenario.json`, import.meta.url,
    ), 'utf8'));
    assert.deepEqual(JSON.parse(JSON.stringify(compiled)), native);

    // Removing only unbound definitions must leave the same scalar runtime settings.
    let scalarSource = valid.source;
    const declarations = parse(valid.source).declarations.filter(item => item.distribution).reverse();
    for (const declaration of declarations) {
      scalarSource = scalarSource.slice(0, declaration.range.start.offset)
        + scalarSource.slice(declaration.range.end.offset);
    }
    delete compiled.fixed.randomProfiles;
    assert.deepEqual(compiled, compileScenario(scalarSource));
  });
}
