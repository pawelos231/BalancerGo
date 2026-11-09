import assert from 'node:assert/strict';
import test from 'node:test';

import { applyScenarioField, scenarioFormValues, validateScenarioField } from '../../../src/scenarios/draft.ts';
import { editScenarioForm } from '../../../src/scenarios/formState.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

// Older JSON keeps its shape while the form displays the immediate-ACK defaults.
test('delayed ACK defaults and edits preserve exact virtual durations', () => {
  const original = scenarioFixture();
  const fields = scenarioFormValues(original);
  assert.equal(fields.delayedAckEnabled, 'false');
  assert.equal(fields.delayedAckSegments, '2');
  assert.equal(fields.delayedAckMaxDelayMs, '40');
  assert.deepEqual(original.fixed.tcp, {});

  let edited = applyScenarioField(original, 'delayedAckEnabled', 'true');
  edited = applyScenarioField(edited, 'delayedAckMaxDelayMs', '40.000001');
  edited = applyScenarioField(edited, 'delayedAckSegments', '1');
  assert.deepEqual(edited.fixed.tcp?.delayedAck, {
    enabled: true,
    maxDelayNanoseconds: 40_000_001,
    segmentsPerACK: 1,
  });
  const restored = JSON.parse(serializeScenario(edited));
  assert.equal(scenarioFormValues(restored).delayedAckMaxDelayMs, '40.000001');
  assert.deepEqual(edited.fixed.network, original.fixed.network);
});

// Zero is invalid for delay/threshold, but the cwnd cap uses zero as uncapped.
test('ACK limits reject invalid values and correcting a field clears its error', () => {
  for (const raw of ['0', '3', '-1', '1.5']) {
    assert.ok(validateScenarioField('delayedAckSegments', raw));
  }
  for (const raw of ['0', '-1', '500.000001']) {
    assert.ok(validateScenarioField('delayedAckMaxDelayMs', raw));
  }
  for (const raw of ['0.000001', '40', '500']) {
    assert.equal(validateScenarioField('delayedAckMaxDelayMs', raw), undefined);
  }

  const scenario = scenarioFixture();
  const state = { scenario, values: scenarioFormValues(scenario), errors: {} };
  const rejected = editScenarioForm(state, 'delayedAckMaxDelayMs', '501');
  assert.ok(rejected.errors.delayedAckMaxDelayMs);
  const corrected = editScenarioForm(rejected, 'delayedAckMaxDelayMs', '40');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.tcp?.delayedAck?.maxDelayNanoseconds, 40_000_000);
});

// An actual cwnd cap needs a congestion-control algorithm; rwnd is a different limit.
test('the optional cwnd cap preserves zero and reconciles a pending mode change', () => {
  const original = scenarioFixture();
  assert.equal(scenarioFormValues(original).maximumCongestionWindowSegments, '0');
  const capped = applyScenarioField(original, 'maximumCongestionWindowSegments', '1');
  assert.equal(capped.fixed.tcp?.congestionControl?.maximumWindowSegments, 1);
  const cappedState = {
    scenario: capped,
    values: scenarioFormValues(capped),
    errors: {},
  };
  const pendingNone = editScenarioForm(cappedState, 'congestionControl', 'none');
  assert.match(pendingNone.errors.congestionControl!, /maximum congestion window requires/);
  const uncapped = editScenarioForm(pendingNone, 'maximumCongestionWindowSegments', '0');
  assert.deepEqual(uncapped.errors, {});
  assert.equal(uncapped.scenario.fixed.tcp?.congestionControl?.mode, 'none');
  assert.equal(uncapped.scenario.fixed.tcp?.congestionControl?.maximumWindowSegments, 0);
});

test('the one-segment preset fixes cwnd and other presets keep delayed ACK disabled', () => {
  const defaults = scenarioFixture();
  defaults.fixed.tcp = {
    delayedAck: { enabled: true },
    congestionControl: { maximumWindowSegments: 1 },
  };
  for (const preset of scenarioPresets) {
    const scenario = preset.build(defaults);
    if (preset.id === 'delayed-ack-small-window') {
      assert.equal(scenario.fixed.tcp?.delayedAck?.enabled, true);
      assert.equal(scenario.fixed.tcp.delayedAck.maxDelayNanoseconds, 40_000_000);
      assert.equal(scenario.fixed.tcp.congestionControl?.maximumWindowSegments, 1);
      assert.equal(scenario.fixed.tcp.congestionControl?.initialWindowSegments, 1);
      assert.equal(scenario.fixed.tcp.receiveBufferBytes, 65535);
      continue;
    }
    assert.notEqual(scenario.fixed.tcp?.delayedAck?.enabled, true, preset.id);
    assert.equal(scenario.fixed.tcp?.congestionControl?.maximumWindowSegments, undefined, preset.id);
  }
});
