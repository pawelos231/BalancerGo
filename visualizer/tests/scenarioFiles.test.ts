import assert from 'node:assert/strict';
import test from 'node:test';
import {
  MAX_SCENARIO_BYTES,
  readScenarioFile,
  serializeScenario,
} from '../src/scenarios/files.ts';
import type { Scenario } from '../src/scenarios/types.ts';
import { scenarioFixture } from './scenarioFixture.ts';

const utf8 = new TextEncoder();

function textFile(text: string, size = utf8.encode(text).byteLength): Pick<File, 'size' | 'text'> {
  return { size, text: async () => text };
}

// A long display name exercises file limits without inventing extra scenario fields.
function scenarioWithName(): Scenario {
  const scenario = scenarioFixture();
  scenario.resources.backends = [{
    id: 'backend-1',
    name: '',
    address: '10.0.0.1',
    port: 80,
    network: { toLoadBalancer: {}, fromLoadBalancer: {} },
  }];
  return scenario;
}

test('declared file sizes above 2 MiB are rejected before reading', async () => {
  let reads = 0;
  const file = {
    size: MAX_SCENARIO_BYTES + 1,
    text: async () => {
      reads += 1;
      return '{}';
    },
  };

  await assert.rejects(readScenarioFile(file), /exceeds the 2 MiB limit/);
  assert.equal(reads, 0);
});

test('the inclusive limit counts UTF-8 bytes rather than JavaScript characters', async () => {
  const exact = 'ą'.repeat(MAX_SCENARIO_BYTES / 2);

  assert.equal(await readScenarioFile(textFile(exact)), exact);

  // Recheck decoded content even when a file-like object understates its size.
  await assert.rejects(readScenarioFile(textFile(`${exact}x`, 1)), /2 MiB/);
});

test('a leading BOM is removed once and counts toward the file size', async () => {
  const content = 'x'.repeat(MAX_SCENARIO_BYTES - 3);
  const exact = `\uFEFF${content}`;

  assert.equal(utf8.encode(exact).byteLength, MAX_SCENARIO_BYTES);
  assert.equal(await readScenarioFile(textFile(exact)), content);
  await assert.rejects(readScenarioFile(textFile(`${exact}x`, 0)), /2 MiB/);

  assert.equal(await readScenarioFile(textFile('\uFEFF\uFEFF{}')), '\uFEFF{}');
  assert.equal(await readScenarioFile(textFile(' \uFEFF{}\n')), ' \uFEFF{}\n');
  assert.equal(await readScenarioFile(textFile('{"name":"a\uFEFFb"}')), '{"name":"a\uFEFFb"}');
});

test('empty files and read failures give readable errors and allow another read', async () => {
  for (const text of ['', ' \t\n', '\uFEFF', '\uFEFF \n']) {
    await assert.rejects(readScenarioFile(textFile(text)), /file is empty/);
  }

  let reads = 0;
  const file = {
    size: 2,
    text: async () => {
      reads += 1;

      if (reads === 1) {
        throw new Error('The file became unavailable.');
      }

      return '{}';
    },
  };

  await assert.rejects(readScenarioFile(file), /Could not read.*Select it again/);
  assert.equal(await readScenarioFile(file), '{}');
  assert.equal(reads, 2);
});

test('raw duplicate keys, unsafe integers and fractional tokens reach backend validation unchanged', async () => {
  const documents = [
    ' \n{"version":1,"version":2}\n',
    '{"execution":{"seed":9007199254740993}}',
    '{"execution":{"durationNanoseconds":1.25}}',
    '{"execution":{"maxEvents":1e20}}',
    '{ invalid JSON that only the backend should parse',
  ];

  for (const document of documents) {
    assert.equal(await readScenarioFile(textFile(document)), document);
  }
});

test('pretty exports round trip the complete scenario with precise strings and explicit zero overrides', async () => {
  const scenario = scenarioWithName();
  scenario.resources.backends[0].name = 'Serwer Żółć';
  scenario.resources.backends[0].network.profile = 'old-profile';
  scenario.network.profiles?.push({
    id: 'zero-queue',
    config: { queueCapacity: 0, packetTravelTimeNanoseconds: 0, lossRate: 0 },
  });
  scenario.workload.clientGroups = [{
    id: 'clients',
    count: 1,
    startAtNanoseconds: 0,
    clientGapNanoseconds: 0,
    network: {
      profile: 'old-profile',
      toLoadBalancer: { queueCapacity: 0 },
      fromLoadBalancer: {},
    },
    stream: { packetGapNanoseconds: 0 },
    overrides: [{
      index: 0,
      network: {
        profile: 'zero-queue',
        toLoadBalancer: {},
        fromLoadBalancer: { lossRate: 0 },
      },
      stream: { packets: 0, bytes: 0, packetGapNanoseconds: 0 },
    }],
  }];
  const original = structuredClone(scenario);

  const exported = serializeScenario(scenario);
  const imported = await readScenarioFile(textFile(exported));

  assert.equal(exported, `${JSON.stringify(scenario, null, 2)}\n`);
  assert.deepEqual(JSON.parse(imported), original);
  assert.deepEqual(scenario, original);
  assert.match(exported, /"seed": "-9007199254740993"/);
  assert.match(exported, /"maxEvents": "18446744073709551615"/);
  assert.match(exported, /"durationNanoseconds": "9223372036854775807"/);
});

test('pretty JSON exactly at the UTF-8 limit keeps its trailing newline', () => {
  const scenario = scenarioWithName();
  const baseBytes = utf8.encode(`${JSON.stringify(scenario, null, 2)}\n`).byteLength;
  scenario.resources.backends[0].name = 'x'.repeat(MAX_SCENARIO_BYTES - baseBytes);

  const exported = serializeScenario(scenario);

  assert.equal(utf8.encode(exported).byteLength, MAX_SCENARIO_BYTES);
  assert.equal(exported, `${JSON.stringify(scenario, null, 2)}\n`);
});

test('compact fallback accepts exactly 2 MiB including multibyte text', async () => {
  const scenario = scenarioWithName();
  const baseBytes = utf8.encode(JSON.stringify(scenario)).byteLength;
  const remaining = MAX_SCENARIO_BYTES - baseBytes;
  scenario.resources.backends[0].name = 'ą'.repeat(Math.floor(remaining / 2))
    + 'x'.repeat(remaining % 2);

  const exported = serializeScenario(scenario);

  assert.equal(utf8.encode(exported).byteLength, MAX_SCENARIO_BYTES);
  assert.equal(exported, JSON.stringify(scenario));
  assert.deepEqual(JSON.parse(await readScenarioFile(textFile(exported))), scenario);

  scenario.resources.backends[0].name += 'ą';
  assert.throws(() => serializeScenario(scenario), /2 MiB export limit/);
});

test('unserializable scenarios report an export error', () => {
  const scenario = scenarioFixture();
  Object.defineProperty(scenario, 'toJSON', {
    value() {
      throw new Error('Serialization failed.');
    },
  });

  assert.throws(() => serializeScenario(scenario), /Could not export.*Validate it/);
});
