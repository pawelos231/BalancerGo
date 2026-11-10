import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi } from '../../../src/scenarios/api.ts';
import {
  applyScenarioField, rebaseScenarioCount, scenarioFormValues, scenarioStateTimes,
} from '../../../src/scenarios/draft.ts';
import { validateEditorDocument } from '../../../src/scenarios/editor.ts';
import { readScenarioFile, serializeScenario } from '../../../src/scenarios/files.ts';
import { editScenarioForm, retryScenarioForm, selectScenarioFormState } from '../../../src/scenarios/formState.ts';
import {
  applyHostToAllBackends, editHostField, emptyHostEdits, retryHostEdits,
} from '../../../src/scenarios/host.ts';
import { editScenarioTimeline } from '../../../src/scenarios/timelineEdits.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

const summary = { totalClients: 2, backendCount: 1, clientGroupCount: 1 };

function file(document: string): Pick<File, 'size' | 'text'> {
  return { size: new TextEncoder().encode(document).byteLength, text: async () => document };
}

test('a duration edit cannot adopt a document whose later state reaches the new boundary', () => {
  const scenario = scenarioFixture();
  scenario.fixed.execution.durationNanoseconds = 20_000_000_000;
  scenario.states['10000000000'] = structuredClone(scenario.states['0']);
  const original = selectScenarioFormState(validateScenario(scenario), '10000000000');
  const before = structuredClone(original);

  const rejected = editScenarioForm(original, 'durationSeconds', '10');

  assert.equal(rejected.scenario, original.scenario);
  assert.equal(rejected.selectedAt, '10000000000');
  assert.equal(rejected.values.durationSeconds, '10');
  assert.match(rejected.errors.durationSeconds!, /timestamp|duration/i);
  assert.deepEqual(structuredClone(original), before);
  assert.doesNotThrow(() => validateScenario(rejected.scenario));

  const corrected = editScenarioForm(rejected, 'durationSeconds', '10.000000001');
  assert.deepEqual(corrected.errors, {});
  assert.equal(corrected.scenario.fixed.execution.durationNanoseconds, 10_000_000_001);
  assert.equal(corrected.selectedAt, '10000000000');
});

test('RTO ordering errors retain rejected text and retry after related successful edits', () => {
  const original = selectScenarioFormState(validateScenario(scenarioFixture()));
  const originalTCP = structuredClone(original.scenario.fixed.tcp);
  const minimum = editScenarioForm(original, 'rtoMinimumMs', '2000');
  assert.equal(minimum.scenario, original.scenario);
  assert.equal(minimum.values.rtoMinimumMs, '2000');
  assert.match(minimum.errors.rtoMinimumMs!, /minimum.*initial.*maximum/i);
  assert.deepEqual(original.scenario.fixed.tcp, originalTCP);
  assert.equal(original.scenario.fixed.tcp.rto?.minimumTimeoutNanoseconds, undefined);

  const initial = editScenarioForm(minimum, 'rtoInitialMs', '3000');
  assert.deepEqual(initial.errors, {});
  assert.deepEqual(initial.scenario.fixed.tcp.rto, {
    initialTimeoutNanoseconds: 3_000_000_000,
    minimumTimeoutNanoseconds: 2_000_000_000,
  });

  const maximum = editScenarioForm(initial, 'rtoMaximumMs', '1000');
  assert.equal(maximum.scenario, initial.scenario);
  assert.equal(maximum.values.rtoMaximumMs, '1000');
  assert.ok(maximum.errors.rtoMaximumMs);

  const lowerMinimum = editScenarioForm(maximum, 'rtoMinimumMs', '500');
  assert.ok(lowerMinimum.errors.rtoMaximumMs);
  assert.equal(lowerMinimum.scenario.fixed.tcp.rto?.maximumTimeoutNanoseconds, undefined);
  const corrected = editScenarioForm(lowerMinimum, 'rtoInitialMs', '1000');
  assert.deepEqual(corrected.errors, {});
  assert.deepEqual(corrected.scenario.fixed.tcp.rto, {
    initialTimeoutNanoseconds: 1_000_000_000,
    minimumTimeoutNanoseconds: 500_000_000,
    maximumTimeoutNanoseconds: 1_000_000_000,
  });
  assert.doesNotThrow(() => validateScenario(corrected.scenario));
});

test('consecutive count digits retain an applied duration retry and reject incompatible restored hosts', () => {
  let scenario = applyScenarioField(scenarioFixture(), 'durationSeconds', '1');
  scenario = applyScenarioField(scenario, 'servers', '2');
  scenario.states['0'].resources.backends[1].host = { packetProcessingTimeNanoseconds: 1 };
  const pending = editScenarioForm(
    selectScenarioFormState(scenario), 'durationSeconds', '9223372036.854775807',
  );
  assert.ok(pending.errors.durationSeconds);
  const baseline = pending.scenario;

  const firstDigit = editScenarioForm(pending, 'servers', '1', baseline);
  assert.deepEqual(firstDigit.errors, {});
  assert.equal(firstDigit.scenario.fixed.execution.durationNanoseconds, '9223372036854775807');

  // Production refreshes this baseline after accepted retries; an older baseline
  // must also preserve the latest duration instead of restoring the original one.
  const refreshed = rebaseScenarioCount(baseline, firstDigit.scenario, 'servers');
  for (const activeBaseline of [baseline, refreshed]) {
    const secondDigit = retryScenarioForm(
      editScenarioForm(firstDigit, 'servers', '10', activeBaseline), activeBaseline,
    );

    assert.equal(secondDigit.scenario, firstDigit.scenario);
    assert.equal(secondDigit.scenario.fixed.execution.durationNanoseconds, '9223372036854775807');
    assert.equal(secondDigit.values.durationSeconds, '9223372036.854775807');
    assert.equal(secondDigit.values.servers, '10');
    assert.deepEqual(Object.keys(secondDigit.errors), ['servers']);
    assert.match(secondDigit.errors.servers!, /host|duration/i);
    const projected = scenarioFormValues(secondDigit.scenario);
    assert.equal(projected.durationSeconds, secondDigit.values.durationSeconds);
    assert.equal(projected.servers, '1');
    assert.doesNotThrow(() => validateScenario(secondDigit.scenario));
  }
});

test('count baselines retain accepted host changes and removed custom backends across shrink and grow', () => {
  let baseline = applyScenarioField(scenarioFixture(), 'durationSeconds', '1');
  baseline = applyScenarioField(baseline, 'servers', '2');
  baseline.states['0'].resources.backends[1].name = 'Custom backend';
  baseline.states['0'].resources.backends[1].network.fromLoadBalancer.queueCapacity = 19;
  let form = editScenarioForm(selectScenarioFormState(baseline), 'servers', '1', baseline);
  const retainedID = form.scenario.states['0'].resources.backends[0].id;
  const acceptedHost = editHostField(
    form.scenario, emptyHostEdits(), `backend:${retainedID}`, 'packetProcessingTimeUs', '10',
  );
  assert.deepEqual(acceptedHost.edits.errors, {});
  form = { ...form, scenario: acceptedHost.scenario };
  baseline = rebaseScenarioCount(baseline, form.scenario, 'servers');
  form = editScenarioForm(form, 'servers', '10', baseline);
  assert.deepEqual(form.errors, {});
  assert.equal(form.scenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 10000);
  assert.equal(form.scenario.states['0'].resources.backends[1].name, 'Custom backend');
  assert.equal(form.scenario.states['0'].resources.backends[1].network.fromLoadBalancer.queueCapacity, 19);

  baseline = rebaseScenarioCount(baseline, form.scenario, 'servers');
  form = editScenarioForm(form, 'servers', '0', baseline);
  baseline = rebaseScenarioCount(baseline, form.scenario, 'servers');
  form = editScenarioForm(form, 'servers', '2', baseline);
  assert.deepEqual(form.errors, {});
  assert.equal(form.scenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 10000);
  assert.equal(form.scenario.states['0'].resources.backends[1].name, 'Custom backend');
  assert.equal(form.values.servers, scenarioFormValues(form.scenario).servers);
});

test('count rebasing keeps original client proportions and the latest unrelated accepted settings', () => {
  let baseline = applyScenarioField(scenarioFixture(), 'durationSeconds', '1');
  baseline = applyScenarioField(baseline, 'clients', '3');
  const groups = baseline.states['0'].workload.clientGroups;
  groups.push({ ...structuredClone(groups[0]), id: 'other-clients', count: 6 });
  let form = editScenarioForm(selectScenarioFormState(baseline), 'clients', '1', baseline);
  assert.deepEqual(form.scenario.states['0'].workload.clientGroups.map(group => group.count), [0, 1]);
  form = editScenarioForm(form, 'seed', '42');
  form = editScenarioForm(form, 'clients', '18', baseline);
  assert.deepEqual(form.errors, {});
  assert.deepEqual(form.scenario.states['0'].workload.clientGroups.map(group => group.count), [6, 12]);
  assert.equal(form.scenario.fixed.execution.seed, 42);
  assert.equal(form.values.seed, scenarioFormValues(form.scenario).seed);
});

test('host edits keep whole-document failures pending until the duration permits them', () => {
  const original = validateScenario(scenarioFixture());
  const before = structuredClone(original);
  const rejected = editHostField(original, emptyHostEdits(), 'loadBalancer', 'packetProcessingTimeUs', '1');

  assert.equal(rejected.scenario, original);
  assert.equal(rejected.edits.values.loadBalancer.packetProcessingTimeUs, '1');
  assert.match(rejected.edits.errors['loadBalancer/packetProcessingTimeUs'], /host|duration/i);
  assert.deepEqual(structuredClone(original), before);
  assert.doesNotThrow(() => validateScenario(rejected.scenario));

  const shorter = editScenarioForm(selectScenarioFormState(rejected.scenario), 'durationSeconds', '1');
  assert.deepEqual(shorter.errors, {});
  // The coordinator retries retained host input after a related form edit succeeds.
  const corrected = retryHostEdits(shorter.scenario, rejected.edits);
  assert.deepEqual(corrected.edits.errors, {});
  assert.deepEqual(corrected.scenario.states['0'].resources.loadBalancer.host, {
    packetProcessingTimeNanoseconds: 1000,
  });
  assert.doesNotThrow(() => validateScenario(corrected.scenario));
});

test('common backend host settings reject an invalid complete candidate without changing any host', () => {
  const scenario = validateScenario(applyScenarioField(scenarioFixture(), 'servers', '2'));
  const before = structuredClone(scenario);

  assert.throws(() => applyHostToAllBackends(scenario, {
    cores: '1', packetProcessingTimeUs: '1', queueCapacity: '1024',
  }), /host|duration/i);

  assert.deepEqual(structuredClone(scenario), before);
  assert.ok(scenario.states['0'].resources.backends.every(backend => Object.keys(backend.host ?? {}).length === 0));
  assert.doesNotThrow(() => validateScenario(scenario));
});

test('file, JSON, form, host, timeline, preview, export and Start retain one exact native document', async () => {
  let source = applyScenarioField(scenarioFixture(), 'servers', '1');
  source = applyScenarioField(source, 'clients', '2');
  const initial = source.states['0'];
  initial.resources.backends[0].network = {
    profile: 'old-profile', toLoadBalancer: { queueCapacity: 0 }, fromLoadBalancer: { lossRate: null },
  };
  initial.workload.clientGroups[0].stream = { packets: 3, bytes: null, packetGapNanoseconds: 0 };
  initial.workload.clientGroups[0].overrides = [{
    index: 1,
    network: { profile: '', toLoadBalancer: { queueCapacity: 0 }, fromLoadBalancer: { lossRate: 0 } },
    stream: { bytes: 123 },
  }];
  for (const at of ['9007199254740992', '9007199254740993']) {
    source.states[at] = structuredClone(initial);
  }
  source = validateScenario(source);
  const before = structuredClone(source);
  const document = ` \n${JSON.stringify(source, null, 2).replace('"speed": 0', '"speed": 0.0')}\n`;
  const requests: { url: string; document: string }[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const body = options?.body as string;
    requests.push({ url: String(url), document: body });
    assert.equal(options?.method, 'POST');
    assert.deepEqual(options?.headers, { 'Content-Type': 'application/json' });
    const scenario = validateScenario(JSON.parse(body));
    return String(url).endsWith('/validate')
      ? Response.json({ scenario, summary })
      : Response.json({ runId: 'precise-draft-run', status: 'accepted' }, { status: 202 });
  });

  const importedText = await readScenarioFile(file(`\uFEFF${document}`));
  const imported = await validateEditorDocument(importedText, api, new AbortController().signal);
  assert.equal(requests[0].document, document);
  let form = editScenarioForm({
    scenario: imported.scenario,
    selectedAt: '9007199254740993',
    values: scenarioFormValues(imported.scenario, '9007199254740993'),
    errors: {},
  }, 'bandwidthBitsPerSecond', '9007199254740993');
  form = editScenarioForm(form, 'algorithm', 'roundRobin');
  assert.deepEqual(form.errors, {});

  let host = editHostField(form.scenario, emptyHostEdits(), 'loadBalancer', 'queueCapacity', '0', form.selectedAt);
  host = editHostField(host.scenario, host.edits, 'loadBalancer', 'packetProcessingTimeUs', '9007199254740.993', form.selectedAt);
  assert.deepEqual(host.edits.errors, {});
  const timeline = editScenarioTimeline(host.scenario, form.selectedAt!, {
    type: 'copy', seconds: '9007199.254740994',
  });
  const preview = validateScenario(timeline.scenario);
  const exported = serializeScenario(preview);
  const reimported = await validateEditorDocument(
    await readScenarioFile(file(exported)), api, new AbortController().signal,
  );
  const receipt = await api.start(reimported.scenario);

  assert.deepEqual(receipt, { runId: 'precise-draft-run', status: 'accepted' });
  assert.deepEqual(requests.map(request => request.url), [
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/simulations',
  ]);
  assert.equal(requests[1].document, exported);
  const started = JSON.parse(requests[2].document) as Scenario;
  assert.equal(Object.hasOwn(started, 'scenario'), false);
  assert.deepEqual(started, structuredClone(preview));
  assert.deepEqual(reimported.scenario, structuredClone(preview));
  assert.deepEqual(scenarioStateTimes(started), [
    '0', '9007199254740992', '9007199254740993', '9007199254740994',
  ]);
  assert.equal(started.fixed.execution.seed, '-9007199254740993');
  assert.equal(started.fixed.execution.maxEvents, '18446744073709551615');
  assert.equal(started.fixed.execution.durationNanoseconds, '9223372036854775807');
  assert.equal(started.fixed.network.defaults.bandwidthBitsPerSecond, '9007199254740993');
  assert.deepEqual(started.fixed.tcp, before.fixed.tcp);
  assert.equal(Object.hasOwn(started.fixed.tcp, 'mssBytes'), false);
  assert.equal(Object.hasOwn(started.states['0'].resources.loadBalancer.host ?? {}, 'packetProcessingTimeNanoseconds'), false);
  const copied = started.states['9007199254740994'];
  assert.equal(copied.resources.loadBalancer.algorithm, 'roundRobin');
  assert.deepEqual(copied.resources.loadBalancer.host, {
    queueCapacity: 0, packetProcessingTimeNanoseconds: '9007199254740993',
  });
  assert.deepEqual(copied.resources.backends[0].network, before.states['0'].resources.backends[0].network);
  assert.deepEqual(copied.workload.clientGroups, before.states['0'].workload.clientGroups);
  assert.deepEqual(structuredClone(source), before);
});

test('an aborted validation cannot replace a newer applied draft when its response arrives last', async () => {
  const original = validateScenario(scenarioFixture());
  const edited = editScenarioForm(selectScenarioFormState(original), 'algorithm', 'roundRobin').scenario;
  const responders: ((response: Response) => void)[] = [];
  const api = new ScenarioApi('http://backend.test', () => new Promise<Response>(resolve => {
    responders.push(resolve);
  }));
  const obsoleteController = new AbortController();
  let applied = original;
  const obsolete = validateEditorDocument(serializeScenario(original), api, obsoleteController.signal)
    .then(result => { applied = result.scenario; });
  const obsoleteRejection = assert.rejects(obsolete, { name: 'AbortError' });
  obsoleteController.abort();

  const current = validateEditorDocument(serializeScenario(edited), api, new AbortController().signal)
    .then(result => { applied = result.scenario; });
  responders[1](Response.json({ scenario: edited, summary }));
  await current;
  assert.deepEqual(applied, edited);

  // The transport deliberately ignores cancellation and resolves the old request last.
  responders[0](Response.json({ scenario: original, summary }));
  await obsoleteRejection;
  assert.deepEqual(applied, edited);
});
