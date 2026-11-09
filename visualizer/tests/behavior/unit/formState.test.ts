import assert from 'node:assert/strict';
import test from 'node:test';

import { applyScenarioField, scenarioFormValues } from '../../../src/scenarios/draft.ts';
import { editScenarioForm } from '../../../src/scenarios/formState.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

function formWithMode(mode: string) {
  const scenario = applyScenarioField(scenarioFixture(), 'congestionControl', mode);
  return { scenario, values: scenarioFormValues(scenario), errors: {} };
}

// The rejected SACK choice stays visible under Tahoe, then becomes real native data under Reno.
test('selecting Reno applies pending SACK and clears its error', () => {
  const original = formWithMode('tahoe');
  const rejected = editScenarioForm(original, 'sackEnabled', 'true');
  assert.match(rejected.errors.sackEnabled!, /SACK requires/);
  assert.equal(rejected.values.sackEnabled, 'true');
  assert.equal(rejected.scenario.fixed.tcp?.sackEnabled, undefined);

  const corrected = editScenarioForm(rejected, 'congestionControl', 'reno');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.values.sackEnabled, 'true');
  assert.equal(corrected.scenario.fixed.tcp?.sackEnabled, true);
  assert.equal(corrected.scenario.fixed.tcp?.congestionControl?.mode, 'reno');
  assert.equal(original.values.sackEnabled, 'false');
});

// Turning SACK off also makes an earlier request for timeout-only recovery valid.
test('disabling SACK applies pending fast-retransmit changes', () => {
  const enabled = editScenarioForm(formWithMode('reno'), 'sackEnabled', 'true');
  const rejected = editScenarioForm(enabled, 'fastRetransmit', 'false');
  assert.match(rejected.errors.fastRetransmit!, /SACK requires/);

  const corrected = editScenarioForm(rejected, 'sackEnabled', 'false');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.tcp?.congestionControl?.fastRetransmit, false);
  assert.equal(corrected.values.fastRetransmit, 'false');
});

// SACK cannot be retried successfully until the other pending recovery edit is applied.
test('related pending choices resolve together after selecting a supported mode', () => {
  const sack = editScenarioForm(formWithMode('none'), 'sackEnabled', 'true');
  const recovery = editScenarioForm(sack, 'fastRetransmit', 'true');
  assert.ok(recovery.errors.sackEnabled);
  assert.ok(recovery.errors.fastRetransmit);

  const corrected = editScenarioForm(recovery, 'congestionControl', 'reno');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.tcp?.sackEnabled, true);
  assert.equal(corrected.scenario.fixed.tcp?.congestionControl?.fastRetransmit, true);
  assert.equal(corrected.values.fastRetransmit, 'true');
});

// Reno fixes the mode requirement, but an explicitly disabled fast retransmit still blocks SACK.
test('the remaining SACK error names the dependency that is still missing', () => {
  const rejected = editScenarioForm(formWithMode('none'), 'sackEnabled', 'true');
  assert.match(rejected.errors.sackEnabled!, /Reno, NewReno or CUBIC/);

  const supportedMode = editScenarioForm(rejected, 'congestionControl', 'reno');
  assert.equal(supportedMode.errors.sackEnabled, 'SACK requires fast retransmit to be enabled.');
  assert.equal(supportedMode.scenario.fixed.tcp?.congestionControl?.fastRetransmit, false);

  const corrected = editScenarioForm(supportedMode, 'fastRetransmit', 'true');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.tcp?.sackEnabled, true);
});

// Fixing transport dependencies must not hide an unrelated invalid numeric input.
test('recovering related fields preserves unrelated errors and precise native values', () => {
  const invalidBuffer = editScenarioForm(formWithMode('tahoe'), 'receiveBufferBytes', '0');
  const sack = editScenarioForm(invalidBuffer, 'sackEnabled', 'true');
  const corrected = editScenarioForm(sack, 'congestionControl', 'reno');
  assert.deepEqual(Object.keys(corrected.errors), ['receiveBufferBytes']);
  assert.equal(corrected.values.receiveBufferBytes, '0');
  assert.equal(corrected.scenario.fixed.tcp?.receiveBufferBytes, undefined);
  assert.equal(corrected.scenario.fixed.execution.seed, '-9007199254740993');

  const validBuffer = editScenarioForm(corrected, 'receiveBufferBytes', '65535');
  assert.deepEqual(validBuffer.errors, {});
});

// Choosing none again cannot erase an unresolved request to enable fast retransmit.
test('mode edits keep unresolved requested values and their errors', () => {
  const rejected = editScenarioForm(formWithMode('none'), 'fastRetransmit', 'true');
  const unchanged = editScenarioForm(rejected, 'congestionControl', 'none');
  assert.ok(unchanged.errors.fastRetransmit);
  assert.equal(unchanged.values.fastRetransmit, 'true');
  assert.equal(unchanged.scenario.fixed.tcp?.congestionControl?.fastRetransmit, false);

  // An explicit timeout-only choice remains intact when no edit is pending.
  const timeoutOnly = editScenarioForm(formWithMode('none'), 'congestionControl', 'reno');
  assert.deepEqual(timeoutOnly.errors, {});
  assert.equal(timeoutOnly.values.fastRetransmit, 'false');
});

// A rejected mode change can become valid through SACK instead of editing that select again.
test('disabling SACK applies a pending mode and refreshes its dependent recovery display', () => {
  const enabled = editScenarioForm(formWithMode('reno'), 'sackEnabled', 'true');
  const rejected = editScenarioForm(enabled, 'congestionControl', 'none');
  assert.ok(rejected.errors.congestionControl);

  const corrected = editScenarioForm(rejected, 'sackEnabled', 'false');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.tcp?.congestionControl?.mode, 'none');
  assert.equal(corrected.values.congestionControl, 'none');
  assert.equal(corrected.values.fastRetransmit, 'false');
});
