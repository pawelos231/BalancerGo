import assert from 'node:assert/strict';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import { filterLibraryEntries, libraryCategories, sourceExampleNotice } from '../../../src/scenarios/library.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { sourceExamples } from '../../../src/scenarios/sourceExamples.ts';

// Every source offered by default must pass the same checker used in the editor.
test('ordinary library entries all compile and pass semantic checks', () => {
  const entries = filterLibraryEntries('all', '');
  const definitions = sourceExamples.filter(example => !example.expectedDiagnostic);
  assert.equal(entries.length, scenarioPresets.length + definitions.length);

  for (const entry of entries) {
    assert.equal(checkLanguageSource(entry.source), null, entry.id);
    assert.doesNotThrow(() => compileScenario(entry.source), entry.id);
  }

  const category = libraryCategories.find(item => item.id === 'all')!;
  assert.equal(category.count, entries.length);
  assert.deepEqual(
    filterLibraryEntries('distributions', '').map(item => item.id),
    definitions.map(item => item.id),
  );
});

// Negative tests stay available, but neither default browsing nor search exposes them.
test('rejected sources require the explicit diagnostics filter', () => {
  const examples = sourceExamples.filter(example => example.expectedDiagnostic);
  const diagnostics = filterLibraryEntries('diagnostics', '');
  assert.deepEqual(diagnostics.map(item => item.id), examples.map(item => item.id));
  assert.equal(filterLibraryEntries('all', 'invalid-parameters').length, 0);
  assert.deepEqual(
    filterLibraryEntries('diagnostics', 'normal-invalid-parameters').map(item => item.id),
    ['normal-invalid-parameters', 'lognormal-invalid-parameters'],
  );

  for (const example of examples) {
    assert.ok(checkLanguageSource(example.source)?.message.endsWith(example.expectedDiagnostic!), example.id);
  }
});

test('loaded notices distinguish valid definitions from expected diagnostics', () => {
  const valid = sourceExamples.find(example => example.id === 'normal-profile-definitions')!;
  const invalid = sourceExamples.find(example => example.id === 'normal-invalid-parameters')!;

  assert.match(sourceExampleNotice(valid), /Compile and apply/);
  assert.doesNotMatch(sourceExampleNotice(valid), /not runnable|Expected diagnostic/);
  assert.ok(sourceExampleNotice(invalid).includes(invalid.expectedDiagnostic!));
});
