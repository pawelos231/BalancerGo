import assert from 'node:assert/strict';
import test from 'node:test';

import {
  applyHostToAllBackends, backendHostTarget, commonBackendHostValues,
  editHostField, emptyHostEdits, hostValues, retainHostEdits, validateHostValue,
} from '../../../src/scenarios/host.ts';
import { applyScenarioField } from '../../../src/scenarios/draft.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';
import { nodeHostState, showHostCPUWork } from '../../../src/topology/telemetry.ts';
import { formatMetric } from '../../../src/topology/format.ts';

function backendsScenario() {
  const scenario = applyScenarioField(scenarioFixture(), 'servers', '2');
  // Positive CPU costs reserve work over the horizon; keep these edits in range.
  return applyScenarioField(scenario, 'durationSeconds', '1');
}

// Omitted CPU is ideal. Setting 10 microseconds writes exactly 10,000 nanoseconds.
test('host settings preserve omission and edit only their selected native host', () => {
  const original = backendsScenario();
  assert.deepEqual(hostValues(original.states['0'].resources.loadBalancer.host), {
    cores: '1', packetProcessingTimeUs: '0', queueCapacity: '1024',
  });
  const backendID = original.states['0'].resources.backends[1].id;
  const changed = editHostField(
    original, emptyHostEdits(), backendHostTarget(backendID), 'packetProcessingTimeUs', '10',
  );

  assert.deepEqual(changed.edits.errors, {});
  assert.equal(changed.scenario.states['0'].resources.backends[1].host?.packetProcessingTimeNanoseconds, 10_000);
  assert.deepEqual(changed.scenario.states['0'].resources.backends[0], original.states['0'].resources.backends[0]);
  assert.deepEqual(changed.scenario.states['0'].resources.loadBalancer, original.states['0'].resources.loadBalancer);
  assert.equal(original.states['0'].resources.backends[1].host, undefined);
});

// Invalid input stays visible and cannot mutate the accepted document.
test('host validation keeps explicit zeroes and clears corrected pending edits', () => {
  for (const [field, invalid] of [
    ['cores', '0'], ['cores', '1025'], ['queueCapacity', '-1'],
    ['queueCapacity', '1000001'], ['packetProcessingTimeUs', '0.0001'],
  ] as const) assert.ok(validateHostValue(field, invalid), `${field}=${invalid}`);

  assert.equal(validateHostValue('queueCapacity', '0'), undefined);
  assert.equal(validateHostValue('packetProcessingTimeUs', '0'), undefined);
  assert.equal(validateHostValue('packetProcessingTimeUs', '0.001'), undefined);

  const original = backendsScenario();
  const invalid = editHostField(original, emptyHostEdits(), 'loadBalancer', 'cores', '0');
  assert.equal(invalid.scenario, original);
  assert.equal(invalid.edits.values.loadBalancer.cores, '0');
  assert.ok(invalid.edits.errors['loadBalancer/cores']);

  const corrected = editHostField(original, invalid.edits, 'loadBalancer', 'cores', '2');
  assert.deepEqual(corrected.edits.errors, {});
  assert.equal(corrected.scenario.states['0'].resources.loadBalancer.host?.cores, 2);
});

// Imported heterogeneous hosts stay distinct until Apply to all explicitly replaces them.
test('common backend controls detect mixed settings and apply all without sharing objects', () => {
  const original = backendsScenario();
  original.states['0'].resources.backends[0].host = { cores: 1, packetProcessingTimeNanoseconds: 10000, queueCapacity: 8 };
  original.states['0'].resources.backends[1].host = { cores: 4, packetProcessingTimeNanoseconds: 10000, queueCapacity: 32 };
  assert.deepEqual(commonBackendHostValues(original), {
    cores: '', packetProcessingTimeUs: '10', queueCapacity: '',
  });

  const updated = applyHostToAllBackends(original, {
    cores: '2', packetProcessingTimeUs: '0', queueCapacity: '0',
  });
  assert.deepEqual(commonBackendHostValues(updated), {
    cores: '2', packetProcessingTimeUs: '0', queueCapacity: '0',
  });
  updated.states['0'].resources.backends[0].host!.cores = 3;
  assert.equal(updated.states['0'].resources.backends[1].host!.cores, 2);
  assert.equal(original.states['0'].resources.backends[0].host.cores, 1);
});

// Resizing preserves retained CPU settings and removes invisible invalid drafts.
test('backend resize retains host settings and prunes removed backend errors', () => {
  const original = backendsScenario();
  original.states['0'].resources.backends[0].host = { cores: 4, packetProcessingTimeNanoseconds: 10000 };
  const removedID = original.states['0'].resources.backends[1].id;
  const invalid = editHostField(original, emptyHostEdits(), backendHostTarget(removedID), 'cores', '0');
  const reduced = applyScenarioField(original, 'servers', '1');
  assert.deepEqual(reduced.states['0'].resources.backends[0].host, original.states['0'].resources.backends[0].host);
  assert.deepEqual(retainHostEdits(reduced, invalid.edits), emptyHostEdits());

  const expanded = applyScenarioField(original, 'servers', '3');
  assert.deepEqual(expanded.states['0'].resources.backends[0].host, original.states['0'].resources.backends[0].host);
  assert.equal(expanded.states['0'].resources.backends[2].host, undefined);
});

// Library presets deliberately choose CPU experiments or reset inherited CPU limits.
test('CPU presets select only their intended hosts and all other presets disable CPU', () => {
  const defaults = backendsScenario();
  defaults.states['0'].resources.loadBalancer.host = { cores: 9, packetProcessingTimeNanoseconds: 999999 };
  for (const backend of defaults.states['0'].resources.backends) backend.host = { packetProcessingTimeNanoseconds: 999999 };

  for (const preset of scenarioPresets) {
    const scenario = preset.build(defaults);
    if (preset.category !== 'host-cpu') {
      assert.equal(hostValues(scenario.states['0'].resources.loadBalancer.host).packetProcessingTimeUs, '0', preset.id);
      for (const backend of scenario.states['0'].resources.backends) {
        assert.equal(hostValues(backend.host).packetProcessingTimeUs, '0', preset.id);
      }
    }
  }

  const preset = scenarioPresets.find(entry => entry.id === 'backend-cpu-bottleneck')!;
  const configured = preset.build(defaults);
  assert.equal(configured.fixed.tcp?.congestionControl?.mode, 'newReno');
  assert.equal(configured.fixed.tcp?.sackEnabled, true);
  assert.equal(hostValues(configured.states['0'].resources.loadBalancer.host).packetProcessingTimeUs, '0');
  assert.ok(configured.states['0'].resources.backends.every(backend => hostValues(backend.host).packetProcessingTimeUs === '10'));
});

// Backend cards and topology use one set of counters from the generic node metrics.
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
