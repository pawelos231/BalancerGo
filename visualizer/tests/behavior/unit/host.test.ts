import assert from 'node:assert/strict';
import test from 'node:test';

import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import type { ScenarioHostPatch } from '../../../src/scenarios/types.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { nodeHostState, showHostCPUWork } from '../../../src/topology/telemetry.ts';
import { formatMetric } from '../../../src/topology/format.ts';
import { fixtureBackends, scenarioFixture } from '../../scenarioFixture.ts';

function backendsScenario() {
  const scenario = scenarioFixture();
  scenario.fixed.execution.durationNanoseconds = 1_000_000_000;
  scenario.states['0'].resources.backends = fixtureBackends(2);
  return scenario;
}

test('host JSON retains omitted settings, distinct backend hosts and exact nanoseconds', () => {
  const original = backendsScenario();
  assert.deepEqual(validateScenario(original).states['0'].resources.loadBalancer.host, {});

  const scenario = structuredClone(original);
  scenario.states['0'].resources.backends[0].host = { cores: 1, packetProcessingTimeNanoseconds: 10000, queueCapacity: 8 };
  scenario.states['0'].resources.backends[1].host = { cores: 4, packetProcessingTimeNanoseconds: '9007199254740993', queueCapacity: 0 };
  const restored = parseScenario(serializeScenario(scenario));

  assert.deepEqual(restored.states['0'].resources.backends, validateScenario(scenario).states['0'].resources.backends);
  assert.deepEqual(restored.states['0'].resources.loadBalancer.host, {});
  assert.equal(original.states['0'].resources.backends[0].host, undefined);
  assert.equal(original.states['0'].resources.backends[1].host, undefined);
});

test('host JSON validates cores, queue capacity and processing cost for every host', () => {
  const invalidHosts: ScenarioHostPatch[] = [
    { cores: 0 }, { cores: 1025 }, { cores: 1.5 },
    { queueCapacity: -1 }, { queueCapacity: 1000001 },
    { packetProcessingTimeNanoseconds: -1 }, { packetProcessingTimeNanoseconds: 0.1 },
  ];
  for (const host of invalidHosts) {
    for (const target of ['loadBalancer', 'backend'] as const) {
      const scenario = backendsScenario();
      const resource = target === 'loadBalancer'
        ? scenario.states['0'].resources.loadBalancer
        : scenario.states['0'].resources.backends[0];
      resource.host = host;
      assert.throws(() => parseScenario(JSON.stringify(scenario)), /host\./, `${target}: ${JSON.stringify(host)}`);
    }
  }

  const scenario = backendsScenario();
  scenario.states['0'].resources.loadBalancer.host = { cores: 1024, packetProcessingTimeNanoseconds: 0, queueCapacity: 0 };
  scenario.states['0'].resources.backends[0].host = { cores: 1, packetProcessingTimeNanoseconds: 1, queueCapacity: 1000000 };
  assert.doesNotThrow(() => validateScenario(scenario));
});

test('host queue and core budgets are checked against the complete simulation duration', () => {
  const scenario = scenarioFixture();
  scenario.states['0'].resources.loadBalancer.host = { packetProcessingTimeNanoseconds: 1 };
  const before = structuredClone(scenario);

  assert.throws(() => validateScenario(scenario), /host.*duration/);
  assert.deepEqual(scenario, before);

  scenario.fixed.execution.durationNanoseconds = 1_000_000_000;
  assert.doesNotThrow(() => validateScenario(scenario));
});

test('CPU presets select only their intended hosts and all other presets disable CPU', () => {
  for (const preset of scenarioPresets) {
    const scenario = compileScenario(preset.source);
    if (preset.category !== 'host-cpu') {
      assert.equal(scenario.states['0'].resources.loadBalancer.host?.packetProcessingTimeNanoseconds, 0, preset.id);
      for (const backend of scenario.states['0'].resources.backends) {
        assert.equal(backend.host?.packetProcessingTimeNanoseconds, 0, preset.id);
      }
    }
  }

  const preset = scenarioPresets.find(entry => entry.id === 'backend-cpu-bottleneck')!;
  const configured = compileScenario(preset.source);
  assert.equal(configured.fixed.tcp.congestionControl?.mode, 'newReno');
  assert.equal(configured.fixed.tcp.sackEnabled, true);
  assert.equal(configured.states['0'].resources.loadBalancer.host?.packetProcessingTimeNanoseconds, 0);
  assert.ok(configured.states['0'].resources.backends.every(backend => backend.host?.packetProcessingTimeNanoseconds === 10000));
});

test('host telemetry retains occupancy and formats virtual utilization as a percentage', () => {
  const values = {
    hostCPUEnabled: 1, hostCores: 2, hostBusyCores: 1,
    hostQueueCapacity: 8, hostQueuedPackets: 3, hostMaxQueuedPackets: 8,
    hostProcessedPackets: 1000, hostQueueDroppedPackets: 12, hostUtilization: 0.875,
  };
  const host = nodeHostState({
    id: 'backend/one', kind: 'backend', label: 'One',
    metrics: Object.entries(values).map(([key, value]) => ({ key, label: key, value })),
  });
  assert.ok(host?.enabled);
  assert.equal(host.busyCores, 1);
  assert.equal(host.queuedPackets, 3);
  assert.equal(host.queueDroppedPackets, 12);
  assert.equal(formatMetric({ key: 'hostUtilization', label: 'CPU', value: host.utilization }), '87.5%');
  assert.equal(nodeHostState(undefined), undefined);
});

test('zero-cost host telemetry retains earlier utilization and retiring work', () => {
  const values = {
    hostCPUEnabled: 0, hostCores: 1, hostBusyCores: 2,
    hostUtilization: 2 / 3, hostBusyTimeSeconds: 2, hostAvailableCoreTimeSeconds: 3,
  };
  const host = nodeHostState({
    id: 'backend/one', kind: 'backend', label: 'One',
    metrics: Object.entries(values).map(([key, value]) => ({ key, label: key, value })),
  });
  assert.ok(host);
  assert.equal(host.enabled, false);
  assert.equal(host.busyCores, 2);
  assert.equal(host.availableCoreTimeSeconds, 3);
  assert.equal(host.busyTimeSeconds, 2);
  assert.equal(showHostCPUWork(host), true);
  assert.equal(showHostCPUWork({ ...host, busyCores: 0 }), true);

  const legacy = nodeHostState({
    id: 'backend/one', kind: 'backend', label: 'One',
    metrics: [{ key: 'hostCPUEnabled', label: 'CPU', value: 0 }],
  });
  assert.ok(legacy);
  assert.equal(legacy.availableCoreTimeSeconds, undefined);
  assert.equal(showHostCPUWork(legacy), false);
});
