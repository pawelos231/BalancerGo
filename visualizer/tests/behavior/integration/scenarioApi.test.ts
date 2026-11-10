import assert from 'node:assert/strict';
import test from 'node:test';
import { ScenarioApi, ScenarioApiError, resolveApiBase } from '../../../src/scenarios/api.ts';
import { experimentFixture, scenarioFixture } from '../../scenarioFixture.ts';
import { topologyDefinition } from '../../telemetryFixture.ts';
import { telemetrySource } from '../../../src/telemetry/exact.ts';

test('HTTP topology keeps exact large limits separately from numeric live settings', async () => {
  const definition = topologyDefinition();
  definition.links[0].config.bandwidthBitsPerSecond = 43;
  const json = JSON.stringify(definition).replace(':43', ':9007199254740993');
  const api = new ScenarioApi('http://backend.test', async () => new Response(json));

  const received = await api.topology();
  assert.equal(typeof received.links[0].config.bandwidthBitsPerSecond, 'number');
  assert.equal(telemetrySource(received).links[0].config.bandwidthBitsPerSecond, '9007199254740993');
});

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
    assert.equal(body.fixed.execution.seed, '-9007199254740993');
    assert.equal(body.fixed.execution.maxEvents, '18446744073709551615');
    assert.equal('scenario' in body, false);
  }
});

test('current run keeps its exact scenario and experiment context, and 404 represents no run', async () => {
  const scenario = scenarioFixture();
  for (const status of ['accepted', 'running', 'finished', 'failed']) {
    const run = { runId: 'c0fda683-opaque-id', status, scenario, experiment: experimentFixture() };
    const existing = new ScenarioApi('http://backend.test', async () => Response.json(run));
    assert.deepEqual(await existing.current(), run);
  }
  const missing = new ScenarioApi('http://backend.test', async () => (
    Response.json({ error: 'No simulation exists.' }, { status: 404 })
  ));
  assert.equal(await missing.current(), null);
});

test('current run rejects missing or malformed experiment context and empty run IDs', async () => {
  const run = {
    runId: 'opaque-id', status: 'running', scenario: scenarioFixture(), experiment: experimentFixture(),
  };
  const invalidSpeed = experimentFixture();
  invalidSpeed.execution.speed = -1;
  const invalidBuild = { ...experimentFixture(), build: { goVersion: 'go1.25.0' } };
  for (const experiment of [undefined, null, {}, invalidSpeed, invalidBuild]) {
    const api = new ScenarioApi('http://backend.test', async () => Response.json({ ...run, experiment }));
    await assert.rejects(api.current(), /unexpected response/);
  }
  const emptyId = new ScenarioApi('http://backend.test', async () => Response.json({ ...run, runId: '' }));
  await assert.rejects(emptyId.current(), /unexpected response/);
});

test('file validation sends original JSON tokens and never starts a run', async () => {
  const documents = [
    '{"scenarioVersion":1,"scenarioVersion":2}',
    '{"execution":{"seed":9007199254740993}}',
    '{"execution":{"seed":9007199254740991.1}}',
    '{"execution":{"seed":1e2}}',
    '{"scenarioVersion":1} {"scenarioVersion":2}',
    '{ invalid JSON',
    '',
  ];
  const requests: string[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    assert.equal(String(url), 'http://backend.test/api/scenarios/validate');
    assert.equal(options?.method, 'POST');
    assert.deepEqual(options?.headers, { 'Content-Type': 'application/json' });
    requests.push(options?.body as string);
    return Response.json({ error: 'scenario.fixed.execution.seed: invalid token' }, { status: 400 });
  });

  for (const document of documents) {
    await assert.rejects(api.validateDocument(document), /scenario.fixed.execution.seed: invalid token/);
  }
  assert.deepEqual(requests, documents);
});

test('validation retains native data and a separate exact resolved preview', async () => {
  const scenario = scenarioFixture();
  const summary = { totalClients: 0, backendCount: 0, clientGroupCount: 0 };
  const resolved = {
    scenarioVersion: scenario.scenarioVersion,
    fixed: {
      execution: scenario.fixed.execution, tcp: {}, loadBalancer: scenario.fixed.loadBalancer,
      networkDefaults: scenario.fixed.network.defaults,
      defaultStream: { ...scenario.fixed.workload.defaults, bytes: 99 * 1024 },
    },
    states: [{ atNanoseconds: 0, loadBalancer: scenario.states['0'].resources.loadBalancer,
      backends: [], clientGroups: [], totalClients: 0 }],
    totalClients: 0,
  };
  const api = new ScenarioApi('http://backend.test', async () => Response.json({ scenario, summary, resolved }));

  const validated = await api.validateDocument(JSON.stringify(scenario));
  assert.deepEqual(validated.scenario, scenario);
  assert.deepEqual(validated.resolved, resolved);
  assert.equal(validated.scenario.fixed.workload.defaults.bytes, -1);
  assert.equal(validated.resolved?.fixed.defaultStream.bytes, 99 * 1024);
  assert.equal(validated.resolved?.fixed.execution.seed, '-9007199254740993');

  const invalid = new ScenarioApi('http://backend.test', async () => (
    Response.json({ scenario, summary, resolved: { totalClients: 0 } })
  ));
  await assert.rejects(invalid.validate(scenario), /unexpected response/);

  const unversioned: Record<string, unknown> = { ...resolved };
  delete unversioned.scenarioVersion;
  for (const invalidResolved of [
    { ...unversioned, version: 2 },
    { ...resolved, version: 2 },
    { ...resolved, scenarioVersion: 1 },
  ]) {
    const api = new ScenarioApi('http://backend.test', async () => (
      Response.json({ scenario, summary, resolved: invalidResolved })
    ));
    await assert.rejects(api.validate(scenario), /unexpected response/);
  }
});

test('validation and Start retain authoritative zero queue scalars for omitted and explicit inputs', async () => {
  const requested = scenarioFixture();
  requested.fixed.network.defaults.congestionThresholdPercent = 0;
  requested.fixed.network.defaults.congestionSignalProbability = 0;
  requested.states['0'].resources.loadBalancer.ingress.congestionThresholdPercent = 0;
  requested.states['0'].resources.loadBalancer.ingress.congestionSignalProbability = 0;
  const canonical = structuredClone(requested);
  const summary = { totalClients: 0, backendCount: 0, clientGroupCount: 0 };
  let startedScenario;
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    if (String(url).endsWith('/validate')) return Response.json({ scenario: canonical, summary });
    startedScenario = JSON.parse(options?.body as string);
    return Response.json({ runId: 'run-zero', status: 'accepted' }, { status: 202 });
  });
  const validated = await api.validateDocument(JSON.stringify(requested));
  assert.deepEqual(validated.scenario, requested);
  await api.start(validated.scenario);
  assert.deepEqual(startedScenario, canonical);

  const inherited = await api.validateDocument(JSON.stringify(scenarioFixture()));
  assert.deepEqual(inherited.scenario, canonical);
  assert.equal(inherited.scenario.fixed.network.defaults.congestionThresholdPercent, 0);
  assert.equal(inherited.scenario.states['0'].resources.loadBalancer.ingress.congestionSignalProbability, 0);
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

test('cancelled defaults and validation reject late responses before any completion side effect', async () => {
  for (const operation of ['defaults', 'validate'] as const) {
    let respond!: (response: Response) => void;
    const api = new ScenarioApi('http://backend.test', () => new Promise<Response>(resolve => {
      respond = resolve;
    }));
    const controller = new AbortController();
    let completions = 0;
    const request = operation === 'defaults'
      ? api.defaults(controller.signal)
      : api.validate(scenarioFixture(), controller.signal);
    const completed = request.then(() => { completions++; });
    const reason = new DOMException('Draft disposed.', 'AbortError');
    controller.abort(reason);
    // Ignore cancellation and return an invalid body, as a late transport may do.
    respond(Response.json({ obsolete: true }));
    await assert.rejects(completed, error => error === reason);
    assert.equal(completions, 0);
  }
});


test('scenario responses reject scenarioVersion one and an invalid later state', async () => {
  const unsupported = { ...scenarioFixture(), scenarioVersion: 1 };
  const unversioned: Record<string, unknown> = { ...scenarioFixture() };
  delete unversioned.scenarioVersion;
  const late = scenarioFixture();
  late.states['10'] = structuredClone(late.states['0']);
  late.states['10'].resources.loadBalancer.forwardingBitsPerSecond = 0;
  for (const scenario of [
    unsupported,
    { ...unversioned, version: 2 },
    { ...scenarioFixture(), version: 2 },
    late,
  ]) {
    const defaults = new ScenarioApi('http://backend.test', async () => Response.json(scenario));
    await assert.rejects(defaults.defaults(), /unexpected response/);
    const validate = new ScenarioApi('http://backend.test', async () => Response.json({
      scenario, summary: { totalClients: 0, backendCount: 0, clientGroupCount: 0 },
    }));
    await assert.rejects(validate.validate(scenarioFixture()), /unexpected response/);
    const current = new ScenarioApi('http://backend.test', async () => Response.json({
      runId: 'run-invalid', status: 'running', scenario, experiment: experimentFixture(),
    }));
    await assert.rejects(current.current(), /unexpected response/);
  }
});
