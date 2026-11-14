import assert from 'node:assert/strict';
import test from 'node:test';
import { initialLanguageSource } from '../../../src/scenario-language/editor.ts';
import { compileScenario } from '../../../src/scenario-language/compile.ts';

// The editor's initial example must remain executable when grammar or defaults change.
test('language editor starts with a complete valid example', () => {
  const scenario = compileScenario(initialLanguageSource);
  assert.equal(scenario.states['0'].resources.backends.length, 2);
  assert.equal(scenario.states['0'].workload.clientGroups[0].count, 8);
  assert.equal(scenario.fixed.tcp.sackEnabled, false);
  assert.equal(scenario.fixed.tcp.ECNenabled, false);
});
