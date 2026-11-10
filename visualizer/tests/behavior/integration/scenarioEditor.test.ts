import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi } from '../../../src/scenarios/api.ts';
import { validateEditorDocument } from '../../../src/scenarios/editor.ts';
import { MAX_SCENARIO_BYTES } from '../../../src/scenarios/files.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

const summary = { totalClients: 0, backendCount: 0, clientGroupCount: 0 };

test('editor validates the full document and sends its original tokens before returning a replacement', async () => {
  const replacement = scenarioFixture();
  replacement.fixed.loadBalancer.id = 'replacement-lb';
  replacement.states['9007199254740993'] = structuredClone(replacement.states['0']);
  replacement.states['9007199254740993'].resources.loadBalancer.algorithm = 'roundRobin';
  const document = ` \n${JSON.stringify(replacement, null, 2).replace('"speed": 0', '"speed": 0.0')}\n `;
  const calls: string[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    assert.equal(String(url), 'http://backend.test/api/scenarios/validate');
    calls.push(options?.body as string);
    return Response.json({ scenario: replacement, summary });
  });

  const result = await validateEditorDocument(document, api, new AbortController().signal);

  assert.deepEqual(calls, [document]);
  assert.deepEqual(result.scenario, replacement);
  assert.equal(result.scenario.fixed.execution.seed, '-9007199254740993');
  assert.ok(Object.hasOwn(result.scenario.states, '9007199254740993'));
  assert.equal(result.scenario.fixed.loadBalancer.id, 'replacement-lb');
});

test('editor rejects invalid JSON and exact-integer errors locally without contacting the server', async () => {
  const document = JSON.stringify(scenarioFixture());
  let requests = 0;
  const api = new ScenarioApi('http://backend.test', async () => {
    requests++;
    throw new Error('Local validation should have rejected this document.');
  });
  const failures: [string, RegExp][] = [
    ['{', /expected an object key|unterminated/],
    [document.replace('"scenarioVersion":2', '"scenarioVersion":2,"scenarioVersion":2'), /duplicate field/],
    [document.replace('"-9007199254740993"', '9007199254740993'), /safe|quoted|exact/i],
    [document.replace('"scenarioVersion":2', '"scenarioVersion":2.1'), /integer/],
    [document.replace('"states":', '"unknownStates":'), /unknown|states/],
  ];

  for (const [text, expected] of failures) {
    await assert.rejects(validateEditorDocument(text, api, new AbortController().signal), expected);
  }
  assert.equal(requests, 0);
});

test('editor counts UTF-8 bytes before parsing, including multibyte text', async () => {
  let requests = 0;
  const api = new ScenarioApi('http://backend.test', async () => {
    requests++;
    throw new Error('An oversized document must stay local.');
  });
  const document = 'ą'.repeat(MAX_SCENARIO_BYTES / 2 + 1);
  assert.ok(document.length < MAX_SCENARIO_BYTES);
  await assert.rejects(
    validateEditorDocument(document, api, new AbortController().signal),
    /2 MiB limit/,
  );
  assert.equal(requests, 0);
});

test('a server rejection does not return a document that could replace the applied draft', async () => {
  const document = JSON.stringify(scenarioFixture());
  const api = new ScenarioApi('http://backend.test', async () => Response.json(
    { error: 'scenario.fixed.execution: rejected by the server' }, { status: 400 },
  ));

  await assert.rejects(
    validateEditorDocument(document, api, new AbortController().signal),
    /scenario.fixed.execution: rejected by the server/,
  );
});

test('an aborted editor request cannot return a late successful server replacement', async () => {
  const scenario = scenarioFixture();
  const controller = new AbortController();
  let respond!: (response: Response) => void;
  const api = new ScenarioApi('http://backend.test', () => new Promise<Response>(resolve => {
    respond = resolve;
  }));
  const result = validateEditorDocument(JSON.stringify(scenario), api, controller.signal);
  controller.abort();
  // Deliberately ignore fetch cancellation, as a late transport can still resolve.
  respond(Response.json({ scenario, summary }));

  await assert.rejects(result, { name: 'AbortError' });
});
