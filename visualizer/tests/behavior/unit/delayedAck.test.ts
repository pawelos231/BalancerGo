import assert from 'node:assert/strict';
import test from 'node:test';

import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

test('delayed ACK JSON retains omission and exact nanosecond durations', () => {
  const original = scenarioFixture();
  assert.deepEqual(validateScenario(original).fixed.tcp.delayedAck, {});

  const scenario = structuredClone(original);
  scenario.fixed.tcp.delayedAck = {
    enabled: true, maxDelayNanoseconds: 40_000_001, segmentsPerACK: 1,
  };
  const restored = parseScenario(serializeScenario(scenario));

  assert.deepEqual(restored.fixed.tcp.delayedAck, scenario.fixed.tcp.delayedAck);
  assert.deepEqual(restored.fixed.network, validateScenario(original).fixed.network);
  assert.deepEqual(original.fixed.tcp, {});
});

test('delayed ACK JSON validates thresholds and delay limits even while disabled', () => {
  for (const segmentsPerACK of [0, 3, -1, 1.5]) {
    const scenario = scenarioFixture();
    scenario.fixed.tcp.delayedAck = { enabled: false, segmentsPerACK };
    assert.throws(() => parseScenario(JSON.stringify(scenario)), /segmentsPerACK/);
  }
  for (const maxDelayNanoseconds of [0, -1, 500_000_001, 1.5]) {
    const scenario = scenarioFixture();
    scenario.fixed.tcp.delayedAck = { enabled: false, maxDelayNanoseconds };
    assert.throws(() => parseScenario(JSON.stringify(scenario)), /maxDelayNanoseconds/);
  }
  for (const maxDelayNanoseconds of [1, 40_000_000, 500_000_000]) {
    for (const segmentsPerACK of [1, 2]) {
      const scenario = scenarioFixture();
      scenario.fixed.tcp.delayedAck = { enabled: true, maxDelayNanoseconds, segmentsPerACK };
      assert.doesNotThrow(() => validateScenario(scenario));
    }
  }
});

test('a nonzero congestion-window cap requires congestion control and zero remains uncapped', () => {
  const scenario = scenarioFixture();
  scenario.fixed.tcp.congestionControl = { mode: 'none', maximumWindowSegments: 1 };
  assert.throws(() => validateScenario(scenario), /maximumWindowSegments.*requires congestion control/);

  scenario.fixed.tcp.congestionControl.maximumWindowSegments = 0;
  const restored = parseScenario(serializeScenario(scenario));
  assert.equal(restored.fixed.tcp.congestionControl?.maximumWindowSegments, 0);
  assert.equal(restored.fixed.tcp.congestionControl?.mode, 'none');
});

test('the one-segment preset fixes cwnd and other presets keep delayed ACK disabled', () => {
  for (const preset of scenarioPresets) {
    const scenario = compileScenario(preset.source);
    if (preset.id === 'delayed-ack-small-window') {
      assert.equal(scenario.fixed.tcp.delayedAck?.enabled, true);
      assert.equal(scenario.fixed.tcp.delayedAck.maxDelayNanoseconds, 40_000_000);
      assert.equal(scenario.fixed.tcp.congestionControl?.maximumWindowSegments, 1);
      assert.equal(scenario.fixed.tcp.congestionControl?.initialWindowSegments, 1);
      assert.equal(scenario.fixed.tcp.receiveBufferBytes, 65535);
      continue;
    }
    assert.equal(scenario.fixed.tcp.delayedAck?.enabled, false, preset.id);
    assert.equal(scenario.fixed.tcp.congestionControl?.maximumWindowSegments, 0, preset.id);
  }
});
