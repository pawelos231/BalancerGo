import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import { validateUniformDistribution } from '../../../src/scenario-language/distributions.ts';
import {
  loadLanguageSource, serializeLanguageSource, validateLanguageDocument,
} from '../../../src/scenario-language/editor.ts';
import { parse } from '../../../src/scenario-language/parser.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';
import type { DistributionExpression, Program, Property } from '../../../src/scenario-language/syntax.ts';
import { sourceExamples } from '../../../src/scenarios/sourceExamples.ts';

const directory = new URL('../../../src/scenarios/examples/sources/', import.meta.url);
const declarationIds = [
  'uniform-cpu-declaration', 'uniform-travel-declaration', 'uniform-timeline-declaration',
];

function distributions(program: Program): DistributionExpression[] {
  const found: DistributionExpression[] = [];
  const visit = (properties: Property[]) => {
    for (const property of properties) {
      if (Array.isArray(property.value)) visit(property.value);
      else if (property.value.kind === 'distribution') found.push(property.value);
    }
  };
  for (const declaration of program.declarations) visit(declaration.properties);
  for (const block of program.timeline) {
    for (const command of block.commands) {
      if (command.kind === 'set') visit(command.properties);
      else if (command.kind === 'add') visit(command.declaration.properties);
    }
  }
  return found;
}

test('distribution catalog uses canonical source files for definitions and runtime drafts', () => {
  const ids = [
    ...declarationIds, 'uniform-invalid-range', 'uniform-invalid-units', 'uniform-missing-bound',
    'uniform-profile-definitions',
  ];
  assert.deepEqual(sourceExamples.map(example => example.id).sort(), ids.sort());
  assert.deepEqual(readdirSync(directory).sort(), ids.map(id => `${id}.lb`).sort());

  for (const example of sourceExamples) {
    assert.equal(example.kind, 'sourceExample');
    assert.equal(example.category, 'distributions');
    assert.equal(example.source, readFileSync(new URL(`${example.id}.lb`, directory), 'utf8'));
    assert.match(example.source, /^language 1$/m);
  }
});

for (const example of sourceExamples.filter(entry => entry.expectedDiagnostic)) {
  test(`${example.id} retains its expected diagnostic through parsing, import and export`, async () => {
    const expressions = distributions(parse(example.source));
    assert.equal(expressions.length, 1);
    const expression = expressions[0];
    assert.equal(expression.family.text, 'uniform');
    if (declarationIds.includes(example.id)) {
      assert.doesNotThrow(() => validateUniformDistribution(expression));
    } else {
      assert.throws(() => validateUniformDistribution(expression), error => {
        assert.ok(error instanceof LanguageError);
        assert.ok(error.message.endsWith(`: ${example.expectedDiagnostic}`));
        return true;
      });
    }

    const diagnostic = checkLanguageSource(example.source);
    assert.ok(diagnostic);
    const { line, column } = diagnostic.range.start;
    assert.equal(diagnostic.message, `${line}:${column}: ${example.expectedDiagnostic}`);
    assert.throws(() => compileScenario(example.source), error => {
      assert.ok(error instanceof LanguageError);
      assert.equal(error.message, diagnostic.message);
      assert.deepEqual(error.range, diagnostic.range);
      return true;
    });

    const expectedToken = example.id === 'uniform-invalid-range' ? '5us'
      : example.id === 'uniform-invalid-units' ? '1KB' : 'uniform';
    assert.equal(example.source.slice(diagnostic.range.start.offset, diagnostic.range.end.offset), expectedToken);

    const exported = serializeLanguageSource(example.source);
    const loaded = await loadLanguageSource({
      size: new TextEncoder().encode(exported).byteLength,
      text: async () => exported,
    }, new AbortController().signal);
    assert.equal(loaded.source, example.source);
    assert.equal(loaded.diagnostic?.message, diagnostic.message);
    assert.deepEqual(loaded.diagnostic?.range, diagnostic.range);
  });
}

// Replace only the future feature to check the surrounding runnable setup and timeline.
test('valid declaration examples compile when their single distribution is replaced by a scalar', () => {
  for (const example of sourceExamples.filter(entry => declarationIds.includes(entry.id))) {
    const expression = distributions(parse(example.source))[0];
    const scalar = example.id === 'uniform-travel-declaration' ? '500us' : '10us';
    const source = example.source.slice(0, expression.range.start.offset) + scalar
      + example.source.slice(expression.range.end.offset);
    const scenario = compileScenario(source);
    assert.equal(scenario.states['0'].resources.backends.length, 1, example.id);
    const group = scenario.states['0'].workload.clientGroups[0];
    assert.equal(group.count, example.id === 'uniform-timeline-declaration' ? 4 : 2, example.id);

    if (example.id === 'uniform-travel-declaration') {
      assert.equal(scenario.fixed.network.defaults.packetTravelTimeNanoseconds, 500_000);
    } else if (example.id === 'uniform-timeline-declaration') {
      assert.equal(scenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 5_000);
      assert.equal(scenario.states['1000000000'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 10_000);
      const firstBirth = BigInt(group.startAtNanoseconds);
      const lastBirth = firstBirth + (BigInt(group.count) - 1n) * BigInt(group.clientGapNanoseconds);
      assert.ok(firstBirth < 1_000_000_000n && 1_000_000_000n < lastBirth);
    } else {
      assert.equal(scenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 10_000);
    }
  }
});

test('applying an inline distribution draft fails before sending native JSON to the API', async () => {
  let requests = 0;
  const api = {
    validate: async () => {
      requests += 1;
      throw new Error('Source examples must not reach native JSON validation.');
    },
  };
  for (const example of sourceExamples.filter(entry => entry.expectedDiagnostic)) {
    await assert.rejects(
      validateLanguageDocument(example.source, api, new AbortController().signal),
      error => {
        assert.ok(error instanceof LanguageError);
        assert.ok(error.message.endsWith(`: ${example.expectedDiagnostic}`));
        return true;
      },
    );
  }
  assert.equal(requests, 0);
});
