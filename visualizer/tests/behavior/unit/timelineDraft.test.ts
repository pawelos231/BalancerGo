import assert from 'node:assert/strict';
import test from 'node:test';
import { applyScenarioField, scenarioFormValues, scenarioStateTimes } from '../../../src/scenarios/draft.ts';
import { editScenarioForm, selectScenarioFormState } from '../../../src/scenarios/formState.ts';
import { editHostField, emptyHostEdits } from '../../../src/scenarios/host.ts';
import { validateScenarioV2 } from '../../../src/scenarios/v2.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

function timeline() {
  const scenario = applyScenarioField(scenarioFixture(), 'clients', '8');
  scenario.fixed.execution.durationNanoseconds = 1000;
  scenario.states['10'] = structuredClone(scenario.states['0']);
  return scenario;
}

test('fixed and selected-state edits retain every other complete state', () => {
  const original = timeline();
  const before = structuredClone(original);
  const edited = applyScenarioField(original, 'algorithm', 'roundRobin', '10');
  assert.equal(edited.states['10'].resources.loadBalancer.algorithm, 'roundRobin');
  assert.deepEqual(edited.states['0'], before.states['0']);
  assert.deepEqual(edited.fixed, before.fixed);
  const fixed = applyScenarioField(edited, 'seed', '42', '10');
  assert.equal(fixed.fixed.execution.seed, 42);
  assert.deepEqual(fixed.states, edited.states);
  const host = editHostField(fixed, emptyHostEdits(), 'loadBalancer', 'cores', '2', '10').scenario;
  assert.equal(host.states['10'].resources.loadBalancer.host?.cores, 2);
  assert.deepEqual(host.states['0'], before.states['0']);
  assert.deepEqual(original, before);
  assert.doesNotThrow(() => validateScenarioV2(host));
});

test('state timestamps sort as exact integers and selecting a state clears pending field text', () => {
  const scenario = timeline();
  scenario.states['2'] = structuredClone(scenario.states['0']);
  scenario.states['9007199254740993'] = structuredClone(scenario.states['0']);
  scenario.states['9007199254740992'] = structuredClone(scenario.states['0']);
  assert.deepEqual(scenarioStateTimes(scenario), ['0', '2', '10', '9007199254740992', '9007199254740993']);
  const rejected = editScenarioForm(selectScenarioFormState(scenario), 'clients', '-1');
  assert.ok(rejected.errors.clients);
  const selected = selectScenarioFormState(rejected.scenario, '10');
  assert.deepEqual(selected.errors, {});
  assert.equal(selected.values.clients, '8');
  const edited = editScenarioForm(selected, 'clients', '4');
  assert.equal(edited.scenario.states['10'].workload.clientGroups[0].count, 4);
  assert.equal(edited.scenario.states['0'].workload.clientGroups[0].count, 8);
  assert.throws(() => scenarioFormValues(scenario, '3'), /does not exist/);
});

test('a later count reduction retains overrides for clients reserved in earlier states', () => {
  const scenario = timeline();
  scenario.states['10'].workload.clientGroups[0].overrides = [{
    index: 7, network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: { bytes: 0 },
  }];
  const edited = applyScenarioField(scenario, 'clients', '2', '10');
  assert.equal(edited.states['10'].workload.clientGroups[0].count, 2);
  assert.deepEqual(edited.states['10'].workload.clientGroups[0].overrides, scenario.states['10'].workload.clientGroups[0].overrides);
  assert.deepEqual(edited.states['0'], scenario.states['0']);
  assert.doesNotThrow(() => validateScenarioV2(edited));
});

test('new resource IDs avoid every state and removed draft IDs even after host edits', () => {
  const scenario = scenarioFixture();
  scenario.fixed.execution.durationNanoseconds = 1000;
  scenario.states['10'] = structuredClone(scenario.states['0']);
  scenario.states['10'].resources.backends = [{
    id: 'backend-1', address: '10.0.0.1', port: 80,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  }];
  const added = applyScenarioField(scenario, 'servers', '1');
  assert.equal(added.states['0'].resources.backends[0].id, 'backend-2');
  const removed = applyScenarioField(added, 'servers', '0');
  const host = editHostField(removed, emptyHostEdits(), 'loadBalancer', 'cores', '1').scenario;
  const replaced = applyScenarioField(host, 'servers', '1');
  assert.equal(replaced.states['0'].resources.backends[0].id, 'backend-3');
  assert.deepEqual(replaced.states['10'], scenario.states['10']);
});

test('a group first inserted in a later state starts at its exact insertion time', () => {
  const scenario = scenarioFixture();
  const at = '9007199254740993';
  scenario.states[at] = structuredClone(scenario.states['0']);
  const edited = applyScenarioField(scenario, 'clients', '3', at);
  assert.equal(edited.states[at].workload.clientGroups[0].startAtNanoseconds, at);
  assert.deepEqual(edited.states['0'], scenario.states['0']);
  assert.doesNotThrow(() => validateScenarioV2(edited));
});
