import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi } from '../../../src/scenarios/api.ts';
import { validateEditorDocument } from '../../../src/scenarios/editor.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { projectPlannedTopology } from '../../../src/scenarios/plannedTopology.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { validateLanguageDocument } from '../../../src/scenario-language/editor.ts';

// Source and JSON can describe different drafts; preview, export and Start follow the accepted native one.
test('language Apply then JSON editing preserves source and uses one native document through Start', async () => {
  const source = '# original source remains separate\nrun 1s\nbalancer lb {}\nbackend api-0 {}\nclients regular { count 2 }';
  const requests: { url: string; body: string }[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const body = options!.body as string;
    requests.push({ url: String(url), body });
    const scenario = validateScenario(JSON.parse(body));
    if (String(url).endsWith('/api/simulations')) {
      return Response.json({ runId: 'coherent-language-run', status: 'accepted' }, { status: 202 });
    }
    const count = Number(scenario.states['0'].workload.clientGroups[0].count);
    return Response.json({ scenario, summary: { totalClients: count, backendCount: 1, clientGroupCount: 1 } });
  });
  const appliedSource = await validateLanguageDocument(source, api, new AbortController().signal);
  const edited = structuredClone(appliedSource.scenario);
  edited.states['0'].workload.clientGroups[0].count = 5;
  const appliedJSON = await validateEditorDocument(JSON.stringify(edited), api, new AbortController().signal);

  const preview = projectPlannedTopology(validateScenario(appliedJSON.scenario), 0n);
  assert.equal(preview.backends.length, 1);
  const exported = serializeScenario(appliedJSON.scenario);
  await api.start(appliedJSON.scenario);
  assert.deepEqual(JSON.parse(requests[2].body), JSON.parse(exported));
  assert.equal(appliedSource.scenario.states['0'].workload.clientGroups[0].count, 2);
  assert.equal(appliedJSON.scenario.states['0'].workload.clientGroups[0].count, 5);
  assert.equal(compileScenario(source).states['0'].workload.clientGroups[0].count, 2);
  assert.ok(source.startsWith('# original source'));
});
