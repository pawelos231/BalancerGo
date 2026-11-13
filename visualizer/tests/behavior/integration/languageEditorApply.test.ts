import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi } from '../../../src/scenarios/api.ts';
import { validateLanguageDocument } from '../../../src/scenario-language/editor.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';
import { validateScenario } from '../../../src/scenarios/validation.ts';

const source = `# preserve this source comment
run 1s seed -9007199254740993 speed 0 max-events 18446744073709551615
defaults link 1EB
balancer lb {}
backend api-0 {}
clients regular { count 2 payload 1KB }
`;
const summary = { totalClients: 2, backendCount: 1, clientGroupCount: 1 };

// Only native JSON reaches HTTP; source comments and large digits are not reinterpreted by Go.
test('language Apply compiles then validates ready native JSON without starting a run', async () => {
  const requests: { url: string; body: string }[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const body = options!.body as string;
    requests.push({ url: String(url), body });
    const scenario = validateScenario(JSON.parse(body));
    return Response.json({ scenario, summary });
  });
  const result = await validateLanguageDocument(source, api, new AbortController().signal);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, 'http://backend.test/api/scenarios/validate');
  assert.equal(result.scenario.scenarioVersion, 2);
  assert.equal(result.scenario.fixed.execution.seed, '-9007199254740993');
  assert.equal(result.scenario.fixed.network.defaults.bandwidthBitsPerSecond, '8000000000000000000');
  assert.equal(requests[0].body.includes('preserve this source comment'), false);
  assert.deepEqual(Object.keys(JSON.parse(requests[0].body)), ['scenarioVersion', 'fixed', 'states']);
});

// A source error never calls HTTP, while a backend rejection never returns a replacement draft.
test('language Apply rejects locally or remotely before returning an applicable scenario', async () => {
  let requests = 0;
  const api = new ScenarioApi('http://backend.test', async () => {
    requests++;
    return Response.json({ error: 'Server rejected this scenario.' }, { status: 400 });
  });
  await assert.rejects(
    validateLanguageDocument(source.replace('1KB', '1GiB'), api, new AbortController().signal),
    LanguageError,
  );
  assert.equal(requests, 0);
  await assert.rejects(validateLanguageDocument(source, api, new AbortController().signal), /Server rejected/);
  assert.equal(requests, 1);
});

// A transport that returns after cancellation cannot publish a late successful response.
test('language Apply checks cancellation before compilation and after validation', async () => {
  const before = new AbortController();
  before.abort();
  let requests = 0;
  const after = new AbortController();
  const api = {
    validate: async (scenario: unknown) => {
      requests++;
      after.abort();
      return { scenario: validateScenario(scenario), summary };
    },
  };
  await assert.rejects(validateLanguageDocument(source, api, before.signal), /abort/i);
  assert.equal(requests, 0);
  await assert.rejects(validateLanguageDocument(source, api, after.signal), /abort/i);
  assert.equal(requests, 1);
});
