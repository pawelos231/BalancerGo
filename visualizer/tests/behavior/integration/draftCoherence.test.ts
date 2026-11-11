import assert from 'node:assert/strict';
import test from 'node:test';

import { ScenarioApi } from '../../../src/scenarios/api.ts';
import { validateEditorDocument } from '../../../src/scenarios/editor.ts';
import { readScenarioFile, serializeScenario } from '../../../src/scenarios/files.ts';
import { projectPlannedTopology } from '../../../src/scenarios/plannedTopology.ts';
import { presetBackends, presetClientGroup } from '../../../src/scenarios/presets/base.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

const summary = { totalClients: 2, backendCount: 1, clientGroupCount: 1 };

function file(document: string): Pick<File, 'size' | 'text'> {
  return { size: new TextEncoder().encode(document).byteLength, text: async () => document };
}

test('invalid cross-field JSON cannot replace the applied document or reach the server', async () => {
  const applied = validateScenario(scenarioFixture());
  applied.states['10'] = structuredClone(applied.states['0']);
  const before = structuredClone(applied);
  let requests = 0;
  const api = new ScenarioApi('http://backend.test', async () => {
    requests++;
    throw new Error('Invalid JSON should have been rejected locally.');
  });

  const short = structuredClone(applied);
  short.fixed.execution.durationNanoseconds = 10;
  await assert.rejects(validateEditorDocument(JSON.stringify(short), api, new AbortController().signal), /timestamp.*below.*duration/);

  const inverted = structuredClone(applied);
  inverted.fixed.tcp.rto = { minimumTimeoutNanoseconds: 2_000_000_000, initialTimeoutNanoseconds: 1_000_000_000 };
  await assert.rejects(
    validateEditorDocument(JSON.stringify(inverted), api, new AbortController().signal),
    /minimum <= initial <= maximum/,
  );

  assert.equal(requests, 0);
  assert.deepEqual(structuredClone(applied), before);
});

test('file import, JSON apply, preview, export and Start retain one exact native document', async () => {
  const source = scenarioFixture();
  const initial = source.states['0'];
  initial.resources.backends = presetBackends(1);
  initial.workload.clientGroups = [presetClientGroup('clients', 2)];
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
  const before = structuredClone(source);
  const document = ` \n${JSON.stringify(source, null, 2).replace('"speed": 0', '"speed": 0.0')}\n`;
  const requests: { url: string; document: string }[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const body = options?.body as string;
    requests.push({ url: String(url), document: body });
    assert.equal(options?.method, 'POST');
    assert.deepEqual(options?.headers, { 'Content-Type': 'application/json' });
    const scenario = JSON.parse(body);
    validateScenario(scenario);
    return String(url).endsWith('/validate')
      ? Response.json({ scenario, summary })
      : Response.json({ runId: 'precise-draft-run', status: 'accepted' }, { status: 202 });
  });

  const importedText = await readScenarioFile(file(`\uFEFF${document}`));
  const imported = await validateEditorDocument(importedText, api, new AbortController().signal);
  assert.equal(requests[0].document, document);

  const edited = structuredClone(imported.scenario);
  edited.fixed.network.defaults.bandwidthBitsPerSecond = '9007199254740993';
  const selected = edited.states['9007199254740993'];
  selected.resources.loadBalancer.algorithm = 'roundRobin';
  selected.resources.loadBalancer.host = {
    queueCapacity: 0, packetProcessingTimeNanoseconds: '9007199254740993',
  };
  edited.states['9007199254740994'] = structuredClone(selected);
  const editedDocument = JSON.stringify(edited, null, 2);
  const applied = await validateEditorDocument(editedDocument, api, new AbortController().signal);
  const preview = projectPlannedTopology(validateScenario(applied.scenario), 9_007_199_254_740_994n);
  assert.equal(preview.stateAtNanoseconds, '9007199254740994');
  assert.equal(preview.backends.length, 1);
  const exported = serializeScenario(applied.scenario);
  const reimported = await validateEditorDocument(
    await readScenarioFile(file(exported)), api, new AbortController().signal,
  );
  const receipt = await api.start(reimported.scenario);

  assert.deepEqual(receipt, { runId: 'precise-draft-run', status: 'accepted' });
  assert.deepEqual(requests.map(request => request.url), [
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/simulations',
  ]);
  assert.equal(requests[1].document, editedDocument);
  assert.equal(requests[2].document, exported);
  const started = JSON.parse(requests[3].document) as Scenario;
  assert.equal(Object.hasOwn(started, 'scenario'), false);
  assert.deepEqual(started, applied.scenario);
  assert.deepEqual(reimported.scenario, applied.scenario);
  assert.deepEqual(Object.keys(started.states), [
    '0', '9007199254740992', '9007199254740993', '9007199254740994',
  ]);
  assert.equal(started.fixed.execution.seed, '-9007199254740993');
  assert.equal(started.fixed.execution.maxEvents, '18446744073709551615');
  assert.equal(started.fixed.execution.durationNanoseconds, '9223372036854775807');
  assert.equal(started.fixed.network.defaults.bandwidthBitsPerSecond, '9007199254740993');
  assert.deepEqual(started.fixed.tcp, imported.scenario.fixed.tcp);
  assert.equal(Object.hasOwn(started.fixed.tcp, 'mssBytes'), false);
  assert.equal(Object.hasOwn(started.states['0'].resources.loadBalancer.host ?? {}, 'packetProcessingTimeNanoseconds'), false);
  const copied = started.states['9007199254740994'];
  assert.equal(copied.resources.loadBalancer.algorithm, 'roundRobin');
  assert.deepEqual(copied.resources.loadBalancer.host, {
    queueCapacity: 0, packetProcessingTimeNanoseconds: '9007199254740993',
  });
  assert.deepEqual(copied.resources.backends[0].network, before.states['0'].resources.backends[0].network);
  assert.deepEqual(copied.workload.clientGroups, before.states['0'].workload.clientGroups);
  assert.deepEqual(source, before);
});

// Go can omit empty arrays: only the topology preview fills in those defaults.
test('a sparse server document can be previewed without adding fields to export or Start', async () => {
  const source = scenarioFixture();
  Reflect.deleteProperty(source.fixed.network, 'profiles');
  const started: Scenario[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const scenario = JSON.parse(options?.body as string) as Scenario;
    validateScenario(scenario);

    if (String(url).endsWith('/validate')) {
      return Response.json({ scenario, summary: { totalClients: 0, backendCount: 0, clientGroupCount: 0 } });
    }
    started.push(scenario);
    return Response.json({ runId: 'sparse-draft-run', status: 'accepted' }, { status: 202 });
  });

  const applied = await validateEditorDocument(serializeScenario(source), api, new AbortController().signal);
  const preview = projectPlannedTopology(validateScenario(applied.scenario), 0n);
  assert.equal(preview.stateAtNanoseconds, '0');
  assert.equal(preview.backends.length, 0);
  assert.equal(Object.hasOwn(applied.scenario.fixed.network, 'profiles'), false);

  const exported = JSON.parse(serializeScenario(applied.scenario)) as Scenario;
  assert.equal(Object.hasOwn(exported.fixed.network, 'profiles'), false);
  await api.start(applied.scenario);
  assert.deepEqual(started, [source]);
});

test('an aborted validation cannot replace a newer applied draft when its response arrives last', async () => {
  const original = validateScenario(scenarioFixture());
  const edited = structuredClone(original);
  edited.states['0'].resources.loadBalancer.algorithm = 'roundRobin';
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

  responders[0](Response.json({ scenario: original, summary }));
  await obsoleteRejection;
  assert.deepEqual(applied, edited);
});
