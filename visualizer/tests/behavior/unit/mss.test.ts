import assert from 'node:assert/strict';
import test from 'node:test';

import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

test('MSS values round trip through JSON without changing workload or filling omitted MSS', () => {
  const original = scenarioFixture();
  const validated = validateScenario(original);
  assert.equal(Object.hasOwn(validated.fixed.tcp, 'mssBytes'), false);

  for (const mssBytes of [536, 1024, 1452, 1460, 8960, 1234]) {
    const scenario = structuredClone(original);
    scenario.fixed.tcp.mssBytes = mssBytes;
    const restored = parseScenario(serializeScenario(scenario));

    assert.equal(restored.fixed.tcp.mssBytes, mssBytes);
    assert.deepEqual(restored.fixed.workload, original.fixed.workload);
    assert.deepEqual(restored.states, validated.states);
  }
  assert.deepEqual(original.fixed.tcp, {});
});

test('MSS JSON rejects invalid values and accepts the supported integer boundaries', () => {
  for (const mssBytes of [0, -1, 65496, 1460.5, 'Ethernet']) {
    const scenario = scenarioFixture();
    scenario.fixed.tcp.mssBytes = mssBytes;
    assert.throws(() => parseScenario(JSON.stringify(scenario)), /mssBytes/, String(mssBytes));
  }

  for (const mssBytes of [1, 1460, 65495]) {
    const scenario = scenarioFixture();
    scenario.fixed.tcp = {
      mssBytes,
      congestionControl: { initialSlowStartThresholdBytes: 2 * mssBytes },
    };
    assert.equal(parseScenario(JSON.stringify(scenario)).fixed.tcp.mssBytes, mssBytes);
  }
});

test('MSS validates the byte threshold and congestion-window arithmetic together', () => {
  const scenario = scenarioFixture();
  scenario.fixed.tcp = { mssBytes: 65495 };
  assert.throws(() => validateScenario(scenario), /initialSlowStartThresholdBytes.*130990/);

  scenario.fixed.tcp.congestionControl = { initialSlowStartThresholdBytes: 130990 };
  assert.doesNotThrow(() => validateScenario(scenario));

  const maximumSegments = ((1n << 63n) - 1n) / 65495n;
  scenario.fixed.tcp.congestionControl.maximumWindowSegments = String(maximumSegments);
  assert.doesNotThrow(() => validateScenario(scenario));
  scenario.fixed.tcp.congestionControl.maximumWindowSegments = String(maximumSegments + 1n);
  assert.throws(() => validateScenario(scenario), /maximumWindowSegments/);
});

test('scenario library starts from its reference MSS after a custom configuration', () => {
  const defaults = scenarioFixture();
  defaults.fixed.tcp = { mssBytes: 8960 };
  for (const preset of scenarioPresets) {
    const expected = preset.category === 'host-cpu' || preset.category === 'timeline' ? 1460 : 1024;
    assert.equal(preset.build(defaults).fixed.tcp.mssBytes ?? 1024, expected, preset.id);
  }
});
