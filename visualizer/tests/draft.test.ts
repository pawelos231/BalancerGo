import assert from 'node:assert/strict';
import test from 'node:test';

import {
  applyScenarioField,
  scenarioFormValues,
  validateScenarioField,
} from '../src/scenarios/draft.ts';
import type { ScenarioField } from '../src/scenarios/draft.ts';
import { scenarioPresets } from '../src/scenarios/presets.ts';
import { scenarioFixture } from './scenarioFixture.ts';

function mixedScenario() {
  const scenario = scenarioPresets[0].build(scenarioFixture());
  scenario.workload.clientGroups[0].network.toLoadBalancer.queueCapacity = 7;
  scenario.workload.clientGroups[0].stream = { bytes: 1234 };
  scenario.workload.clientGroups[0].overrides = [{
    index: 3,
    network: { profile: 'slow-client', toLoadBalancer: {}, fromLoadBalancer: { lossRate: 0.2 } },
    stream: { bytes: 0 },
  }];
  return scenario;
}

test('form projection retains exact native integers and nanosecond precision', () => {
  const scenario = scenarioFixture();
  const fields = scenarioFormValues(scenario);
  assert.equal(fields.durationSeconds, '9223372036.854775807');
  assert.equal(fields.snapshotIntervalMs, '12.345678');
  assert.equal(fields.packetTravelTimeMs, '0.765432');
  assert.equal(fields.ingressPacketTravelTimeMs, '0.000123');
  assert.equal(fields.packetGapMs, '0.000456');
  assert.equal(fields.seed, '-9007199254740993');
  assert.equal(fields.maxEvents, '18446744073709551615');
  assert.equal(fields.bytes, '-1');
  assert.equal(fields.clients, '0');
  assert.equal(fields.servers, '0');
  assert.equal(fields.clientGapMs, '0');

  const mixed = mixedScenario();
  mixed.workload.clientGroups[1].clientGapNanoseconds = '9007199254740993';
  const mixedFields = scenarioFormValues(mixed);
  assert.equal(mixedFields.clients, '16');
  assert.equal(mixedFields.clientGapMs, '');
  mixed.workload.clientGroups[0].clientGapNanoseconds = '9007199254740993';
  assert.equal(scenarioFormValues(mixed).clientGapMs, '9007199254.740993');
});

test('presets populate the same form while one manual edit keeps their native structure', () => {
  const defaults = scenarioFixture();
  for (const preset of scenarioPresets) {
    const scenario = preset.build(defaults);
    const before = structuredClone(scenario);
    const fields = scenarioFormValues(scenario);
    const benchmark = preset.id === 'simulator-throughput';
    assert.equal(fields.durationSeconds, benchmark ? '0.005' : '180');
    assert.equal(fields.bytes, '1073741824');
    assert.equal(fields.servers, benchmark ? '5' : '2');
    let clients = '16';
    if (benchmark) {
      clients = '50';
    } else if (preset.id.endsWith('bottleneck')) {
      clients = '32';
    }
    assert.equal(fields.clients, clients);

    const updated = applyScenarioField(scenario, 'speed', '2.5');
    const expected = structuredClone(scenario);
    expected.execution.speed = 2.5;
    assert.deepEqual(updated, expected);
    assert.deepEqual(scenario, before);
    assert.equal(updated.execution.seed, scenario.execution.seed);
    assert.equal(updated.execution.maxEvents, scenario.execution.maxEvents);
  }
});

test('editing shared defaults preserves profiles, direction patches and client overrides', () => {
  const scenario = mixedScenario();
  const before = structuredClone(scenario);
  let updated = applyScenarioField(scenario, 'bandwidthBitsPerSecond', '9007199254740993');
  updated = applyScenarioField(updated, 'bytes', '-1');
  updated = applyScenarioField(updated, 'packets', '200');
  updated = applyScenarioField(updated, 'algorithm', 'roundRobin');

  const expected = structuredClone(scenario);
  expected.network.defaults.bandwidthBitsPerSecond = '9007199254740993';
  expected.workload.defaults.bytes = -1;
  expected.workload.defaults.packets = 200;
  expected.resources.loadBalancer.algorithm = 'roundRobin';
  assert.deepEqual(updated, expected);
  assert.deepEqual(scenario, before);
  assert.equal(updated.workload.clientGroups[0].stream.bytes, 1234);
  assert.equal(updated.workload.clientGroups[0].overrides![0].stream.bytes, 0);

  const ingress = applyScenarioField(scenario, 'ingressQueueCapacity', '0');
  assert.equal(ingress.resources.loadBalancer.ingress.queueCapacity, 0);
  assert.equal(ingress.network.defaults.queueCapacity, scenario.network.defaults.queueCapacity);
  assert.deepEqual(ingress.workload.clientGroups, scenario.workload.clientGroups);
});

test('time edits use exact decimal arithmetic and retain full Go integer limits', () => {
  let scenario = scenarioFixture();
  scenario = applyScenarioField(scenario, 'durationSeconds', '9223372036.854775807');
  scenario = applyScenarioField(scenario, 'snapshotIntervalMs', '0.000001');
  scenario = applyScenarioField(scenario, 'packetGapMs', '9007199254.740993');
  scenario = applyScenarioField(scenario, 'ingressPacketTravelTimeMs', '9223372036854.775807');
  scenario = applyScenarioField(scenario, 'seed', '-9223372036854775808');
  scenario = applyScenarioField(scenario, 'maxEvents', '18446744073709551615');
  assert.equal(scenario.execution.durationNanoseconds, '9223372036854775807');
  assert.equal(scenario.execution.snapshotIntervalNanoseconds, 1);
  assert.equal(scenario.workload.defaults.packetGapNanoseconds, '9007199254740993');
  assert.equal(scenario.resources.loadBalancer.ingress.packetTravelTimeNanoseconds, '9223372036854775807');
  assert.equal(scenario.execution.seed, '-9223372036854775808');
  assert.equal(scenario.execution.maxEvents, '18446744073709551615');

  const fields = scenarioFormValues(scenario);
  assert.equal(fields.durationSeconds, '9223372036.854775807');
  assert.equal(fields.snapshotIntervalMs, '0.000001');
  assert.equal(fields.packetGapMs, '9007199254.740993');
});

test('client totals scale existing groups deterministically without flattening settings', () => {
  const scenario = mixedScenario();
  const before = structuredClone(scenario);
  const updated = applyScenarioField(scenario, 'clients', '17');
  assert.deepEqual(updated.workload.clientGroups.map((group) => group.count), [9, 8]);
  const expected = structuredClone(scenario);
  expected.workload.clientGroups[0].count = 9;
  assert.deepEqual(updated, expected);
  assert.deepEqual(scenario, before);
  assert.deepEqual(applyScenarioField(scenario, 'clients', '17'), updated);

  scenario.workload.clientGroups[0].count = 12;
  scenario.workload.clientGroups[1].count = 4;
  const proportional = applyScenarioField(scenario, 'clients', '8');
  assert.deepEqual(proportional.workload.clientGroups.map((group) => group.count), [6, 2]);

  const empty = structuredClone(scenario);
  for (const group of empty.workload.clientGroups) {
    group.count = 0;
    group.overrides = [];
  }
  const expanded = applyScenarioField(empty, 'clients', '3');
  assert.deepEqual(expanded.workload.clientGroups.map((group) => group.count), [2, 1]);
  assert.deepEqual(expanded.network.profiles, empty.network.profiles);
});

test('count edits applied from the same baseline retain group ratios and custom backends', () => {
  const baseline = mixedScenario();
  baseline.workload.clientGroups[0].overrides = [];
  baseline.resources.backends[1].name = 'Keep the second backend';
  baseline.resources.backends[1].network.fromLoadBalancer.queueCapacity = 19;
  const before = structuredClone(baseline);

  const intermediateClients = applyScenarioField(baseline, 'clients', '1');
  assert.deepEqual(intermediateClients.workload.clientGroups.map((group) => group.count), [1, 0]);
  const finalClients = applyScenarioField(baseline, 'clients', '18');
  assert.deepEqual(finalClients.workload.clientGroups.map((group) => group.count), [9, 9]);

  const intermediateServers = applyScenarioField(baseline, 'servers', '1');
  assert.equal(intermediateServers.resources.backends.length, 1);
  const finalServers = applyScenarioField(baseline, 'servers', '4');
  assert.deepEqual(finalServers.resources.backends.slice(0, 2), baseline.resources.backends);
  assert.deepEqual(baseline, before);
});

test('client resize refuses to erase explicit overrides and mixed gap edits apply to every group', () => {
  const scenario = mixedScenario();
  scenario.workload.clientGroups[1].clientGapNanoseconds = 20;
  const before = structuredClone(scenario);
  assert.throws(
    () => applyScenarioField(scenario, 'clients', '4'),
    /slow-clients.*at least 4 clients.*explicit overrides/,
  );
  assert.deepEqual(scenario, before);
  assert.equal(scenarioFormValues(scenario).clientGapMs, '');

  const updated = applyScenarioField(scenario, 'clientGapMs', '0.000001');
  assert.deepEqual(updated.workload.clientGroups.map((group) => group.clientGapNanoseconds), [1, 1]);
  const expected = structuredClone(scenario);
  for (const group of expected.workload.clientGroups) group.clientGapNanoseconds = 1;
  assert.deepEqual(updated, expected);
});

test('an empty workload gets a unique group and retains a manually chosen creation gap', () => {
  const scenario = scenarioFixture();
  scenario.resources.loadBalancer.id = 'clients';
  scenario.resources.backends = [{
    id: 'clients-1', address: '10.0.0.1', port: 80,
    network: { toLoadBalancer: {}, fromLoadBalancer: {} },
  }];
  const before = structuredClone(scenario);
  const withGap = applyScenarioField(scenario, 'clientGapMs', '0.000001');
  const updated = applyScenarioField(withGap, 'clients', '2');
  assert.equal(updated.workload.clientGroups.length, 1);
  const group = updated.workload.clientGroups[0];
  assert.equal(group.id, 'clients-2');
  assert.equal(group.count, 2);
  assert.equal(group.clientGapNanoseconds, 1);
  assert.deepEqual(group.network, { toLoadBalancer: {}, fromLoadBalancer: {} });
  assert.deepEqual(group.stream, {});
  assert.deepEqual(scenario, before);
});

test('backend resizing preserves configured prefixes and avoids resource and endpoint collisions', () => {
  const scenario = mixedScenario();
  scenario.resources.loadBalancer.id = 'backend-1';
  scenario.workload.clientGroups[0].id = 'backend-2';
  scenario.resources.backends[0] = {
    id: 'original', name: 'Keep this', address: '::ffff:a00:1', port: '80',
    network: { profile: 'slow-client', toLoadBalancer: { lossRate: 0.3 }, fromLoadBalancer: {} },
  };
  scenario.resources.backends[1].address = '10.0.0.2';
  scenario.resources.backends[1].id = 'another-original';
  const before = structuredClone(scenario);

  const updated = applyScenarioField(scenario, 'servers', '4');
  assert.deepEqual(updated.resources.backends.slice(0, 2), scenario.resources.backends);
  assert.deepEqual(updated.resources.backends.slice(2).map((backend) => [backend.id, backend.address, backend.port]), [
    ['backend-3', '10.0.0.3', 80],
    ['backend-4', '10.0.0.4', 80],
  ]);
  for (const backend of updated.resources.backends.slice(2)) {
    assert.deepEqual(backend.network, { toLoadBalancer: {}, fromLoadBalancer: {} });
  }
  assert.deepEqual(updated.workload, scenario.workload);
  assert.deepEqual(updated.network, scenario.network);
  assert.deepEqual(scenario, before);

  const trimmed = applyScenarioField(updated, 'servers', '1');
  assert.deepEqual(trimmed.resources.backends, [scenario.resources.backends[0]]);
  const empty = applyScenarioField(updated, 'servers', '0');
  assert.deepEqual(empty.resources.backends, []);

  const many = applyScenarioField(scenarioFixture(), 'servers', '300');
  assert.equal(many.resources.backends[253].address, '10.0.0.254');
  assert.equal(many.resources.backends[254].address, '10.0.1.1');
  assert.equal(new Set(many.resources.backends.map((backend) => backend.address)).size, 300);
});

test('field validation rejects overflow, subnanosecond values and malformed integers', () => {
  const valid: [ScenarioField, string][] = [
    ['clients', '16777215'], ['servers', '10000'], ['bytes', '-1'], ['packets', '0'],
    ['seed', '-9223372036854775808'], ['seed', '9223372036854775807'],
    ['maxEvents', '18446744073709551615'], ['bandwidthBitsPerSecond', '9223372036854775807'],
    ['linkQueue', '0'], ['ingressQueueCapacity', '0'], ['speed', '0'], ['speed', '1e3'],
    ['loss', '0'], ['loss', '1'], ['ingressLoss', '0.25'], ['algorithm', 'random'],
    ['durationSeconds', '0.000000001'], ['durationSeconds', '9223372036.854775807'],
    ['snapshotIntervalMs', '0.000001'], ['packetTravelTimeMs', '0'],
  ];
  for (const [field, raw] of valid) {
    assert.equal(validateScenarioField(field, raw), undefined, `${field}=${raw}`);
  }

  const invalid: [ScenarioField, string][] = [
    ['clients', '16777216'], ['servers', '10001'], ['bytes', '-2'], ['packets', '-1'],
    ['seed', '-9223372036854775809'], ['seed', '9223372036854775808'],
    ['maxEvents', '18446744073709551616'], ['maxEvents', '0'],
    ['bandwidthBitsPerSecond', '9223372036854775808'], ['bandwidthBitsPerSecond', '0'],
    ['linkQueue', '-1'], ['ingressQueueCapacity', '1.5'], ['speed', 'Infinity'], ['speed', '-1'],
    ['loss', '1.1'], ['loss', '-0.1'], ['loss', 'NaN'], ['algorithm', 'weightedRandom'],
    ['durationSeconds', '9223372036.854775808'], ['durationSeconds', '0.0000000001'],
    ['snapshotIntervalMs', '0'], ['packetGapMs', '0.0000001'], ['clientGapMs', '-1'],
    ['seed', '1e3'], ['clients', ''], ['seed', '01'],
  ];
  const scenario = scenarioFixture();
  const before = structuredClone(scenario);
  for (const [field, raw] of invalid) {
    assert.ok(validateScenarioField(field, raw), `${field}=${raw}`);
    assert.throws(() => applyScenarioField(scenario, field, raw), Error);
  }
  assert.deepEqual(scenario, before);
});
