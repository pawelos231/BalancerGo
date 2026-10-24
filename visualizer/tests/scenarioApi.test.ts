import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi, ScenarioApiError, resolveApiBase } from '../src/scenarios/api.ts';
import { scenarioFixture } from './scenarioFixture.ts';
import { topologyDefinition } from './telemetryFixture.ts';

test('API address follows the WebSocket backend unless explicitly configured', () => {
  assert.equal(resolveApiBase(undefined, 'ws://localhost:8081/ws'), 'http://localhost:8081');
  assert.equal(resolveApiBase('', 'wss://example.test/prefix/ws?token=1'), 'https://example.test/prefix');
  assert.equal(resolveApiBase('https://api.test/base/', 'ws://other/ws'), 'https://api.test/base');
  assert.throws(() => resolveApiBase('ftp://api.test', 'ws://other/ws'), /HTTP or HTTPS/);
});

test('fetch is called without the API instance as its receiver', async () => {
  const scenario = scenarioFixture();
  const api = new ScenarioApi('http://backend.test', function (this: unknown) {
    assert.equal(this, undefined);
    return Promise.resolve(Response.json(scenario));
  });
  assert.deepEqual(await api.defaults(), scenario);
});

test('validation and Start send the direct document without rounding integer strings', async () => {
  const calls: { url: string; options?: RequestInit }[] = [];
  const scenario = scenarioFixture();
  const summary = { totalClients: 0, backendCount: 0, clientGroupCount: 0 };
  const api = new ScenarioApi('http://backend.test/', async (url, options) => {
    calls.push({ url: String(url), options });
    return String(url).endsWith('/validate')
      ? Response.json({ scenario, summary })
      : Response.json({ runId: 'run-42', status: 'accepted' }, { status: 202 });
  });

  const validated = await api.validate(scenario);
  assert.deepEqual(validated, { scenario, summary });
  assert.deepEqual(await api.start(validated.scenario), { runId: 'run-42', status: 'accepted' });
  assert.deepEqual(calls.map((call) => call.url), [
    'http://backend.test/api/scenarios/validate',
    'http://backend.test/api/simulations',
  ]);
  for (const call of calls) {
    assert.equal(call.options?.method, 'POST');
    assert.deepEqual(call.options?.headers, { 'Content-Type': 'application/json' });
    const body = JSON.parse(call.options?.body as string);
    assert.deepEqual(body, scenario);
    assert.equal(body.execution.seed, '-9007199254740993');
    assert.equal(body.execution.maxEvents, '18446744073709551615');
    assert.equal('scenario' in body, false);
  }
});

test('current run keeps its exact scenario and 404 represents no run', async () => {
  const scenario = scenarioFixture();
  const run = { runId: 'run-1', status: 'finished', scenario };
  const existing = new ScenarioApi('http://backend.test', async () => Response.json(run));
  assert.deepEqual(await existing.current(), run);
  const missing = new ScenarioApi('http://backend.test', async () => (
    Response.json({ error: 'No simulation exists.' }, { status: 404 })
  ));
  assert.equal(await missing.current(), null);
});

test('topology definitions use their separate endpoint and retain the revision token', async () => {
  let requestedUrl = '';
  const definition = topologyDefinition('9007199254740993');
  const api = new ScenarioApi('http://backend.test', async (url) => {
    requestedUrl = String(url);
    return Response.json(definition);
  });
  assert.deepEqual(await api.topology(), definition);
  assert.equal(requestedUrl, 'http://backend.test/api/simulations/current/topology');
});

test('server validation errors, conflicts and invalid responses remain readable', async () => {
  for (const status of [409, 422]) {
    const api = new ScenarioApi('http://backend.test', async () => (
      Response.json({ error: 'Invalid example setting.' }, { status })
    ));
    await assert.rejects(api.start(scenarioFixture()), (error: unknown) => (
      error instanceof ScenarioApiError
      && error.status === status
      && error.message === 'Invalid example setting.'
    ));
  }
  const invalid = new ScenarioApi('http://backend.test', async () => new Response('invalid'));
  await assert.rejects(invalid.defaults(), /invalid JSON response/);
  const missingRunId = new ScenarioApi('http://backend.test', async () => (
    Response.json({ status: 'accepted' }, { status: 202 })
  ));
  await assert.rejects(missingRunId.start(scenarioFixture()), /unexpected response/);
});

test('request cancellation reaches fetch and does not become a timeout message', async () => {
  const controller = new AbortController();
  let requestSignal: AbortSignal | undefined;
  const api = new ScenarioApi('http://backend.test', (_url, options) => {
    requestSignal = options?.signal ?? undefined;
    return new Promise<Response>((_resolve, reject) => {
      requestSignal?.addEventListener('abort', () => reject(requestSignal?.reason), { once: true });
    });
  });
  const request = api.defaults(controller.signal);
  const reason = new DOMException('Cancelled by caller.', 'AbortError');
  controller.abort(reason);
  await assert.rejects(request, (error: unknown) => error === reason);
  assert.equal(requestSignal?.aborted, true);
});
