import assert from 'node:assert/strict';
import test from 'node:test';

import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

test('ECN JSON preserves omission, native spelling and congestion-control dependencies', () => {
  const scenario = scenarioFixture();
  assert.equal(validateScenario(scenario).fixed.tcp.ECNenabled, undefined);

  scenario.fixed.tcp = { ECNenabled: true, congestionControl: { mode: 'none' } };
  assert.throws(() => parseScenario(JSON.stringify(scenario)), /ECNenabled.*requires congestion control/);

  scenario.fixed.tcp.congestionControl!.mode = 'reno';
  assert.equal(parseScenario(serializeScenario(scenario)).fixed.tcp.ECNenabled, true);

  scenario.fixed.tcp = { ECNenabled: false, congestionControl: { mode: 'none' } };
  assert.equal(parseScenario(serializeScenario(scenario)).fixed.tcp.ECNenabled, false);
});

test('threshold queue JSON preserves explicit zeroes and validates both directional ranges', () => {
  const scenario = scenarioFixture();
  const links = [scenario.fixed.network.defaults, scenario.states['0'].resources.loadBalancer.ingress];
  for (const link of links) {
    link.queuePolicy = 'threshold';
    link.congestionThresholdPercent = 0;
    link.congestionSignalProbability = 0;
  }
  const restored = parseScenario(serializeScenario(scenario));
  assert.equal(restored.fixed.network.defaults.congestionThresholdPercent, 0);
  assert.equal(restored.states['0'].resources.loadBalancer.ingress.congestionSignalProbability, 0);

  for (const link of links) {
    for (const value of [0, 100, 12.5]) {
      link.congestionThresholdPercent = value;
      assert.doesNotThrow(() => validateScenario(scenario));
    }
    for (const value of [-1, 100.1, NaN, Infinity]) {
      link.congestionThresholdPercent = value;
      assert.throws(() => validateScenario(scenario), /congestionThresholdPercent/);
    }
    link.congestionThresholdPercent = 0;

    for (const value of [0, 1, 0.25]) {
      link.congestionSignalProbability = value;
      assert.doesNotThrow(() => validateScenario(scenario));
    }
    for (const value of [-1, 1.01, NaN, Infinity]) {
      link.congestionSignalProbability = value;
      assert.throws(() => validateScenario(scenario), /congestionSignalProbability/);
    }
    link.congestionSignalProbability = 0;
  }
});

test('the ECN source isolates its data bottleneck and other sources retain ECN and AQM off', () => {
  for (const preset of scenarioPresets) {
    const scenario = compileScenario(preset.source);
    assert.notEqual(scenario.fixed.network.defaults.queuePolicy, 'threshold', preset.id);
    assert.notEqual(scenario.states['0'].resources.loadBalancer.ingress.queuePolicy, 'threshold', preset.id);
    if (preset.id !== 'ecn-congestion') {
      assert.equal(scenario.fixed.tcp.ECNenabled, false, preset.id);
      continue;
    }
    assert.equal(scenario.fixed.tcp.ECNenabled, true);
    assert.equal(scenario.fixed.execution.durationNanoseconds, 180_000_000_000);
    assert.equal(scenario.fixed.execution.speed, 1);
    assert.equal(scenario.fixed.workload.defaults.bytes, 1_073_741_824);
    for (const backend of scenario.states['0'].resources.backends) {
      assert.equal(backend.network.fromLoadBalancer.queuePolicy, 'threshold');
      assert.equal(backend.network.fromLoadBalancer.congestionSignalProbability, 1);
      assert.deepEqual(backend.network.toLoadBalancer, {});
    }
  }
});
