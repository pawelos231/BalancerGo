import assert from 'node:assert/strict';
import test from 'node:test';
import { applyScenarioField, resourceIds, scenarioStateTimes } from '../../../src/scenarios/draft.ts';
import { editHostField, emptyHostEdits } from '../../../src/scenarios/host.ts';
import { editScenarioTimeline, parseTimelineTimestamp } from '../../../src/scenarios/timelineEdits.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

function timeline(): Scenario {
  const scenario = scenarioFixture();
  scenario.fixed.execution.durationNanoseconds = 100;
  return validateScenario(scenario);
}

function populatedTimeline(): Scenario {
  let scenario = applyScenarioField(timeline(), 'servers', '1');
  scenario = applyScenarioField(scenario, 'clients', '2');
  const state = scenario.states['0'];
  state.resources.backends[0].network.profile = 'old-profile';
  state.resources.backends[0].host = { cores: 2, packetProcessingTimeNanoseconds: 1 };
  state.workload.clientGroups[0].overrides = [{
    index: 1,
    network: { profile: 'old-profile', toLoadBalancer: { lossRate: 0.1 }, fromLoadBalancer: {} },
    stream: { bytes: 123 },
  }];
  scenario.states['10'] = structuredClone(state);
  scenario.states['10'].resources.loadBalancer.forwardingBitsPerSecond = 20_000_000;
  return validateScenario(scenario);
}

test('timeline seconds preserve adjacent nanoseconds above the safe integer range and int64 limits', () => {
  assert.equal(parseTimelineTimestamp('9007199.254740992'), '9007199254740992');
  assert.equal(parseTimelineTimestamp('9007199.254740993'), '9007199254740993');
  assert.equal(parseTimelineTimestamp('9223372036.854775807'), '9223372036854775807');
  assert.equal(parseTimelineTimestamp(' 1.000000000 '), '1000000000');
  assert.equal(parseTimelineTimestamp('0.000000001'), '1');
  for (const text of ['9223372036.854775808', '0.0000000001', '1e-9', '+1', '-1', '01', '']) {
    assert.throws(() => parseTimelineTimestamp(text), Error, text);
  }

  const scenario = validateScenario(scenarioFixture());
  const first = editScenarioTimeline(scenario, '0', { type: 'add', seconds: '9007199.254740993' });
  const second = editScenarioTimeline(first.scenario, first.selectedAt, { type: 'add', seconds: '9007199.254740992' });
  assert.deepEqual(scenarioStateTimes(second.scenario), ['0', '9007199254740992', '9007199254740993']);
  assert.equal(second.selectedAt, '9007199254740992');
});

test('timeline destinations must be unoccupied and strictly below duration', () => {
  const scenario = timeline();
  scenario.states['10'] = structuredClone(scenario.states['0']);
  const before = structuredClone(scenario);
  for (const type of ['add', 'copy', 'move'] as const) {
    for (const seconds of ['0.000000100', '0.000000101', '0.000000010']) {
      assert.throws(() => editScenarioTimeline(scenario, '10', { type, seconds }), Error);
    }
  }
  assert.deepEqual(structuredClone(scenario), before);
  const last = editScenarioTimeline(scenario, '10', { type: 'add', seconds: '0.000000099' });
  assert.equal(last.selectedAt, '99');

  const large = validateScenario(scenarioFixture());
  assert.throws(() => editScenarioTimeline(large, '0', { type: 'add', seconds: '9223372036.854775807' }), Error);
  assert.equal(editScenarioTimeline(large, '0', { type: 'add', seconds: '9223372036.854775806' }).selectedAt, '9223372036854775806');
});

test('the initial state cannot be moved or deleted and missing selection cannot edit a timeline', () => {
  const scenario = timeline();
  const before = structuredClone(scenario);
  assert.throws(() => editScenarioTimeline(scenario, '0', { type: 'delete' }), /0|initial/i);
  assert.throws(() => editScenarioTimeline(scenario, '0', { type: 'move', seconds: '0.000000001' }), /0|initial/i);
  assert.throws(() => editScenarioTimeline(scenario, 'missing', { type: 'add', seconds: '0.000000001' }), Error);
  assert.deepEqual(structuredClone(scenario), before);
});

test('adding a state clones the active predecessor rather than the selected state', () => {
  const scenario = populatedTimeline();
  const before = structuredClone(scenario);
  const result = editScenarioTimeline(scenario, '0', { type: 'add', seconds: '0.000000015' });
  assert.equal(result.selectedAt, '15');
  assert.deepEqual(result.scenario.states['15'], scenario.states['10']);
  assert.deepEqual(result.scenario.states['0'], before.states['0']);
  assert.deepEqual(result.scenario.states['10'], before.states['10']);
  assert.deepEqual(result.scenario.fixed, before.fixed);
  assert.deepEqual(structuredClone(scenario), before);

  result.scenario.states['15'].resources.backends[0].network.toLoadBalancer.queueCapacity = 9;
  result.scenario.states['15'].workload.clientGroups[0].overrides![0].stream.bytes = 456;
  assert.deepEqual(result.scenario.states['10'], before.states['10']);
  assert.deepEqual(structuredClone(scenario), before);
});

test('copying a selected snapshot retains stable IDs, endpoints, profiles and individual overrides', () => {
  const scenario = populatedTimeline();
  const before = structuredClone(scenario);
  const result = editScenarioTimeline(scenario, '0', { type: 'copy', seconds: '0.000000015' });
  assert.equal(result.selectedAt, '15');
  assert.deepEqual(result.scenario.states['15'], scenario.states['0']);
  assert.deepEqual(result.scenario.states['10'], before.states['10']);
  assert.deepEqual(result.scenario.fixed, before.fixed);
  assert.equal(result.scenario.states['15'].resources.backends[0].id, 'backend-1');
  assert.equal(result.scenario.states['15'].workload.clientGroups[0].startAtNanoseconds, 0);
  result.scenario.states['15'].resources.backends[0].host!.cores = 4;
  assert.deepEqual(result.scenario.states['0'], before.states['0']);
  assert.deepEqual(structuredClone(scenario), before);
});

test('copying a removed resource back into the timeline rejects identity resurrection atomically', () => {
  const scenario = applyScenarioField(timeline(), 'servers', '1');
  scenario.states['10'] = structuredClone(scenario.states['0']);
  scenario.states['10'].resources.backends = [];
  const before = structuredClone(scenario);
  const selectedAt = '0';
  assert.throws(() => editScenarioTimeline(scenario, selectedAt, { type: 'copy', seconds: '0.000000020' }), /reus|remov|identit/i);
  assert.deepEqual(structuredClone(scenario), before);
});

test('deleting a state selects its surviving predecessor and preserves the other snapshots', () => {
  const scenario = timeline();
  for (const at of ['10', '20', '30']) scenario.states[at] = structuredClone(scenario.states['0']);
  scenario.states['20'].resources.loadBalancer.forwardingBitsPerSecond = 20_000_000;
  const before = structuredClone(scenario);
  const result = editScenarioTimeline(scenario, '20', { type: 'delete' });
  assert.equal(result.selectedAt, '10');
  assert.deepEqual(scenarioStateTimes(result.scenario), ['0', '10', '30']);
  for (const at of ['0', '10', '30']) assert.deepEqual(result.scenario.states[at], before.states[at]);
  assert.deepEqual(structuredClone(scenario), before);
});

test('moving a state preserves its full snapshot and selects its new chronological position', () => {
  const scenario = populatedTimeline();
  scenario.states['20'] = structuredClone(scenario.states['0']);
  const before = structuredClone(scenario);
  const result = editScenarioTimeline(scenario, '10', { type: 'move', seconds: '0.000000025' });
  assert.equal(result.selectedAt, '25');
  assert.deepEqual(scenarioStateTimes(result.scenario), ['0', '20', '25']);
  assert.deepEqual(result.scenario.states['25'], before.states['10']);
  assert.deepEqual(result.scenario.states['20'], before.states['20']);
  assert.deepEqual(result.scenario.fixed, before.fixed);
  assert.deepEqual(structuredClone(scenario), before);
});

test('delete and move reject a later first group appearance without changing its absolute start', () => {
  const initial = timeline();
  initial.states['10'] = structuredClone(initial.states['0']);
  const scenario = applyScenarioField(initial, 'clients', '2', '10');
  scenario.states['20'] = structuredClone(scenario.states['10']);
  const before = structuredClone(scenario);
  const selectedAt = '10';
  assert.throws(() => editScenarioTimeline(scenario, selectedAt, { type: 'delete' }), /start|insertion/i);
  assert.throws(() => editScenarioTimeline(scenario, selectedAt, { type: 'move', seconds: '0.000000015' }), /start|insertion/i);
  assert.deepEqual(structuredClone(scenario), before);
  assert.equal(scenario.states['10'].workload.clientGroups[0].startAtNanoseconds, 10);
});

test('delete and move keep earlier client budgets required by later sparse overrides', () => {
  const scenario = applyScenarioField(timeline(), 'clients', '1');
  scenario.states['10'] = structuredClone(scenario.states['0']);
  scenario.states['10'].workload.clientGroups[0].count = 10;
  scenario.states['20'] = structuredClone(scenario.states['0']);
  scenario.states['20'].workload.clientGroups[0].overrides = [{
    index: 9, network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: { bytes: 0 },
  }];
  assert.doesNotThrow(() => validateScenario(scenario));
  const before = structuredClone(scenario);
  assert.throws(() => editScenarioTimeline(scenario, '10', { type: 'delete' }), /override|slot|reserv|client/i);
  assert.throws(() => editScenarioTimeline(scenario, '10', { type: 'move', seconds: '0.000000030' }), /override|slot|reserv|client/i);
  assert.deepEqual(structuredClone(scenario), before);
});

test('resource reservations survive timeline edits, validation and subsequent host and form edits', () => {
  const initial = timeline();
  initial.states['10'] = structuredClone(initial.states['0']);
  let scenario = applyScenarioField(initial, 'servers', '1', '10');
  scenario = applyScenarioField(scenario, 'clients', '2', '10');
  scenario.states['20'] = structuredClone(scenario.states['0']);
  const before = structuredClone(scenario);

  let result = editScenarioTimeline(scenario, '10', { type: 'delete' });
  result = editScenarioTimeline(result.scenario, result.selectedAt, { type: 'add', seconds: '0.000000030' });
  result = editScenarioTimeline(result.scenario, result.selectedAt, { type: 'copy', seconds: '0.000000040' });
  result = editScenarioTimeline(result.scenario, result.selectedAt, { type: 'move', seconds: '0.000000050' });
  result = editScenarioTimeline(result.scenario, result.selectedAt, { type: 'delete' });
  assert.equal(result.selectedAt, '30');
  assert.ok(resourceIds(result.scenario).has('backend-1'));
  assert.ok(resourceIds(result.scenario).has('clients'));

  const host = editHostField(result.scenario, emptyHostEdits(), 'loadBalancer', 'cores', '2', result.selectedAt).scenario;
  const withBackend = applyScenarioField(host, 'servers', '1', result.selectedAt);
  const withClients = applyScenarioField(withBackend, 'clients', '3', result.selectedAt);
  assert.equal(withClients.states['30'].resources.backends[0].id, 'backend-2');
  assert.equal(withClients.states['30'].workload.clientGroups[0].id, 'clients-1');
  assert.equal(withClients.states['30'].workload.clientGroups[0].startAtNanoseconds, 30);
  assert.doesNotThrow(() => validateScenario(withClients));
  assert.deepEqual(structuredClone(scenario), before);
});
