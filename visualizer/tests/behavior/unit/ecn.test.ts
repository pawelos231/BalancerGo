import assert from 'node:assert/strict';
import test from 'node:test';
import { applyScenarioField, scenarioFormValues, validateScenarioField } from '../../../src/scenarios/draft.ts';
import { editScenarioForm } from '../../../src/scenarios/formState.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

test('ECN is opt-in, retains the native spelling and reconciles pending mode dependencies', () => {
  const original = scenarioFixture();
  assert.equal(scenarioFormValues(original).ECNenabled, 'false');
  const none = applyScenarioField(original, 'congestionControl', 'none');
  const rejected = editScenarioForm({ scenario: none, values: scenarioFormValues(none), errors: {} }, 'ECNenabled', 'true');
  assert.match(rejected.errors.ECNenabled!, /ECN requires/);
  assert.equal(rejected.scenario.tcp?.ECNenabled, undefined);
  const corrected = editScenarioForm(rejected, 'congestionControl', 'reno');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.tcp?.ECNenabled, true);
  assert.equal(JSON.parse(serializeScenario(corrected.scenario)).tcp.ECNenabled, true);

  const pendingNone = editScenarioForm(corrected, 'congestionControl', 'none');
  assert.match(pendingNone.errors.congestionControl!, /ECN requires/);
  const disabled = editScenarioForm(pendingNone, 'ECNenabled', 'false');
  assert.deepEqual(disabled.errors, {});
  assert.equal(disabled.scenario.tcp?.ECNenabled, false);
  assert.equal(disabled.scenario.tcp?.congestionControl?.mode, 'none');
  assert.equal(original.tcp?.ECNenabled, undefined);
});

test('legacy queue values show zero and activating a threshold initializes only omitted settings', () => {
  const original = scenarioFixture();
  const initial = scenarioFormValues(original);
  assert.equal(initial.queuePolicy, 'tailDrop');
  assert.equal(initial.congestionThresholdPercent, '0');
  assert.equal(initial.congestionSignalProbability, '0');
  const active = editScenarioForm({ scenario: original, values: initial, errors: {} }, 'queuePolicy', 'threshold');
  assert.equal(active.values.congestionThresholdPercent, '80');
  assert.equal(active.values.congestionSignalProbability, '1');
  assert.equal(active.scenario.network.defaults.congestionThresholdPercent, 80);
  assert.equal(original.network.defaults.congestionThresholdPercent, undefined);

  const explicit = structuredClone(original);
  explicit.resources.loadBalancer.ingress.congestionThresholdPercent = 0;
  explicit.resources.loadBalancer.ingress.congestionSignalProbability = 0;
  const preserved = applyScenarioField(explicit, 'ingressQueuePolicy', 'threshold');
  assert.equal(preserved.resources.loadBalancer.ingress.congestionThresholdPercent, 0);
  assert.equal(preserved.resources.loadBalancer.ingress.congestionSignalProbability, 0);
  assert.deepEqual(preserved.network, original.network);
  const reloaded = structuredClone(original);
  reloaded.network.defaults.queuePolicy = 'threshold';
  const off = applyScenarioField(reloaded, 'queuePolicy', 'tailDrop');
  const onAgain = applyScenarioField(off, 'queuePolicy', 'threshold');
  assert.equal(onAgain.network.defaults.congestionThresholdPercent, 0);
  assert.equal(onAgain.network.defaults.congestionSignalProbability, 0);
  const canonicalLegacy = structuredClone(original);
  canonicalLegacy.network.defaults.congestionThresholdPercent = 0;
  canonicalLegacy.network.defaults.congestionSignalProbability = 0;
  const activatedCanonical = applyScenarioField(canonicalLegacy, 'queuePolicy', 'threshold');
  assert.equal(activatedCanonical.network.defaults.congestionThresholdPercent, 0);
  assert.equal(activatedCanonical.network.defaults.congestionSignalProbability, 0);
  for (const field of ['congestionThresholdPercent', 'ingressCongestionThresholdPercent'] as const) {
    for (const raw of ['0', '100', '12.5']) assert.equal(validateScenarioField(field, raw), undefined);
    for (const raw of ['-1', '100.1', 'NaN', 'Infinity']) assert.ok(validateScenarioField(field, raw));
  }
  for (const field of ['congestionSignalProbability', 'ingressCongestionSignalProbability'] as const) {
    for (const raw of ['0', '1', '0.25']) assert.equal(validateScenarioField(field, raw), undefined);
    for (const raw of ['-1', '1.01', 'NaN', 'Infinity']) assert.ok(validateScenarioField(field, raw));
  }
});

test('the ECN preset isolates its data bottleneck and old presets retain ECN and AQM off', () => {
  const defaults = scenarioFixture();
  defaults.tcp = { ECNenabled: true };
  defaults.network.defaults.queuePolicy = 'threshold';
  defaults.resources.loadBalancer.ingress.queuePolicy = 'threshold';
  for (const preset of scenarioPresets) {
    const scenario = preset.build(defaults);
    assert.notEqual(scenario.network.defaults.queuePolicy, 'threshold', preset.id);
    assert.notEqual(scenario.resources.loadBalancer.ingress.queuePolicy, 'threshold', preset.id);
    if (preset.id !== 'ecn-congestion') {
      assert.notEqual(scenario.tcp?.ECNenabled, true, preset.id);
      continue;
    }
    assert.equal(scenario.tcp?.ECNenabled, true);
    assert.equal(scenario.execution.durationNanoseconds, 180_000_000_000);
    assert.equal(scenario.execution.speed, 1);
    assert.equal(scenario.workload.defaults.bytes, 1_073_741_824);
    for (const backend of scenario.resources.backends) {
      assert.equal(backend.network.fromLoadBalancer.queuePolicy, 'threshold');
      assert.equal(backend.network.fromLoadBalancer.congestionSignalProbability, 1);
      assert.deepEqual(backend.network.toLoadBalancer, {});
    }
  }
});
