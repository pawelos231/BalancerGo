import assert from 'node:assert/strict';
import test from 'node:test';

import { applyScenarioField, scenarioFormValues, validateScenarioField } from '../../../src/scenarios/draft.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { editScenarioForm } from '../../../src/scenarios/formState.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

// MSS controls segmentation, while the configured total payload keeps its own units.
test('manual MSS suggestions and custom values round trip without changing workload', () => {
  const original = scenarioFixture();
  assert.equal(scenarioFormValues(original).mssBytes, '1024');
  assert.equal(original.tcp, undefined);

  for (const mss of ['536', '1024', '1452', '1460', '8960', '1234']) {
    const edited = applyScenarioField(original, 'mssBytes', mss);
    assert.equal(edited.tcp?.mssBytes, Number(mss));
    assert.deepEqual(edited.workload, original.workload);

    const restored = JSON.parse(serializeScenario(edited));
    assert.equal(scenarioFormValues(restored).mssBytes, mss);
  }
});

// A mistyped number stays visible until corrected to a supported integer.
test('manual MSS validates numeric limits and clears a corrected input error', () => {
  for (const invalid of ['0', '-1', '65496', '1460.5', 'Ethernet']) {
    assert.ok(validateScenarioField('mssBytes', invalid), invalid);
  }
  for (const valid of ['1', '1460', '65495']) {
    assert.equal(validateScenarioField('mssBytes', valid), undefined, valid);
  }

  const scenario = scenarioFixture();
  const state = { scenario, values: scenarioFormValues(scenario), errors: {} };
  const invalid = editScenarioForm(state, 'mssBytes', '0');
  assert.ok(invalid.errors.mssBytes);
  const corrected = editScenarioForm(invalid, 'mssBytes', '1460');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.tcp?.mssBytes, 1460);
});

// A larger MSS cannot silently raise an explicit byte threshold.
// Fixing that threshold also applies the MSS edit that was waiting on it.
test('MSS-dependent window limits reconcile related pending edits', () => {
  const scenario = scenarioFixture();
  const state = { scenario, values: scenarioFormValues(scenario), errors: {} };
  const large = editScenarioForm(state, 'mssBytes', '65495');
  assert.match(large.errors.mssBytes!, /at least 130990 bytes/);
  assert.equal(large.scenario.tcp, undefined);

  const corrected = editScenarioForm(large, 'initialSlowStartThresholdBytes', '130990');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.tcp?.mssBytes, 65495);

  const small = applyScenarioField(scenario, 'mssBytes', '1');
  assert.equal(validateScenarioField('initialSlowStartThresholdBytes', '2', 1), undefined);
  assert.equal(applyScenarioField(small, 'initialSlowStartThresholdBytes', '2')
    .tcp?.congestionControl?.initialSlowStartThresholdBytes, 2);
});

// A saved custom MSS must not silently alter the library's reference experiments.
test('scenario library starts from the default MSS after a custom configuration', () => {
  const defaults = scenarioFixture();
  defaults.tcp = { mssBytes: 8960 };
  for (const preset of scenarioPresets) {
    const expected = preset.category === 'host-cpu' ? '1460' : '1024';
    assert.equal(scenarioFormValues(preset.build(defaults)).mssBytes, expected, preset.id);
  }
});
