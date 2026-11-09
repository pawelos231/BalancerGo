import assert from 'node:assert/strict';
import test from 'node:test';

import {
  applyScenarioField,
  scenarioFormValues,
  validateScenarioField,
} from '../../../src/scenarios/draft.ts';
import type { ScenarioField } from '../../../src/scenarios/draft.ts';
import { scenarioFieldSections, scenarioSelectFields } from '../../../src/scenarios/formFields.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

function mixedScenario() {
  const preset = scenarioPresets.find((entry) => entry.id === 'mixed-clients');
  assert.ok(preset);
  const scenario = preset.build(scenarioFixture());
  scenario.states['0'].workload.clientGroups[0].network.toLoadBalancer.queueCapacity = 7;
  scenario.states['0'].workload.clientGroups[0].stream = { bytes: 1234 };
  scenario.states['0'].workload.clientGroups[0].overrides = [{
    index: 3,
    network: { profile: 'slow-client', toLoadBalancer: {}, fromLoadBalancer: { lossRate: 0.2 } },
    stream: { bytes: 0 },
  }];
  return scenario;
}

test('SACK is opt-in and preserves omission and explicit false in native documents', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  // Looking at an older scenario does not write the inherited false into it.
  assert.equal(scenarioFormValues(original).sackEnabled, 'false');
  assert.deepEqual(original.fixed.tcp, {});

  const enabled = applyScenarioField(original, 'sackEnabled', 'true');
  assert.deepEqual(enabled.fixed.tcp, { sackEnabled: true });
  assert.equal(scenarioFormValues(enabled).sackEnabled, 'true');

  // Export and a later explicit disable retain the boolean, rather than omit it.
  const disabled = applyScenarioField(enabled, 'sackEnabled', 'false');
  const restored = JSON.parse(serializeScenario(disabled));
  assert.deepEqual(restored.fixed.tcp, { sackEnabled: false });
  assert.equal(scenarioFormValues(restored).sackEnabled, 'false');
  assert.deepEqual(original, before);
  assert.ok(validateScenarioField('sackEnabled', '1'));

  const traffic = scenarioFieldSections.find((section) => section.id === 'tcp-traffic');
  assert.ok(traffic?.fields.includes('sackEnabled'));
});

test('SACK edits reject recovery combinations that the simulator does not support', () => {
  const original = mixedScenario();

  for (const mode of ['reno', 'newReno', 'cubic'] as const) {
    const selected = applyScenarioField(original, 'congestionControl', mode);
    const enabled = applyScenarioField(selected, 'sackEnabled', 'true');
    assert.equal(enabled.fixed.tcp?.sackEnabled, true);
    assert.throws(() => applyScenarioField(enabled, 'fastRetransmit', 'false'), /SACK requires/);
    assert.throws(() => applyScenarioField(enabled, 'congestionControl', 'tahoe'), /SACK requires/);
    assert.throws(() => applyScenarioField(enabled, 'congestionControl', 'none'), /SACK requires/);
  }

  // Timeout-only and Tahoe runs remain available with SACK disabled.
  for (const mode of ['none', 'tahoe'] as const) {
    const selected = applyScenarioField(original, 'congestionControl', mode);
    assert.throws(() => applyScenarioField(selected, 'sackEnabled', 'true'), /SACK requires/);
  }
});

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
  assert.equal(fields.receiveBufferBytes, '65535');
  assert.equal(fields.clients, '0');
  assert.equal(fields.servers, '0');
  assert.equal(fields.clientGapMs, '0');

  const mixed = mixedScenario();
  mixed.states['0'].workload.clientGroups[1].clientGapNanoseconds = '9007199254740993';
  const mixedFields = scenarioFormValues(mixed);
  assert.equal(mixedFields.clients, '16');
  assert.equal(mixedFields.clientGapMs, '');
  mixed.states['0'].workload.clientGroups[0].clientGapNanoseconds = '9007199254740993';
  assert.equal(scenarioFormValues(mixed).clientGapMs, '9007199254.740993');
});

test('receive-buffer edits preserve the scenario and older documents inherit the default', () => {
  const original = mixedScenario();
  assert.deepEqual(original.fixed.tcp, {});
  const before = structuredClone(original);

  const updated = applyScenarioField(original, 'receiveBufferBytes', '64');
  assert.deepEqual(updated.fixed.tcp, { receiveBufferBytes: 64 });
  assert.equal(scenarioFormValues(updated).receiveBufferBytes, '64');
  assert.deepEqual(updated.fixed.network, original.fixed.network);
  assert.deepEqual(updated.states['0'].workload, original.states['0'].workload);
  assert.deepEqual(original, before);

  const restored = JSON.parse(JSON.stringify(updated));
  assert.equal(scenarioFormValues(restored).receiveBufferBytes, '64');
});

test('older scenarios display timer defaults without adding settings to the document', () => {
  const scenario = mixedScenario();
  const before = structuredClone(scenario);

  // Displaying inherited defaults does not turn them into explicit overrides.
  const fields = scenarioFormValues(scenario);
  assert.equal(fields.rtoMode, 'adaptive');
  assert.equal(fields.rtoBackoff, 'true');
  assert.equal(fields.rtoInitialMs, '1000');
  assert.equal(fields.rtoMinimumMs, '200');
  assert.equal(fields.rtoMaximumMs, '60000');
  assert.equal(fields.tcpMSLMs, '30000');
  assert.equal(fields.tcpFinWaitTimeoutMs, '60000');
  assert.equal(fields.tcpMaxFINRetries, '5');
  assert.equal(fields.clientIdleTimeoutMs, '20000');
  assert.equal(fields.backendHalfOpenIdleTimeoutMs, '5000');
  assert.equal(fields.backendEstablishedIdleTimeoutMs, '0');
  assert.equal(fields.lbHalfOpenIdleTimeoutMs, '5000');
  assert.equal(fields.lbEstablishedIdleTimeoutMs, '50000');
  assert.equal(fields.lbClosingIdleTimeoutMs, '120000');
  assert.equal(fields.lbResetIdleTimeoutMs, '10000');
  assert.deepEqual(scenario, before);
});

test('LB mapping timers preserve omission, explicit zero and exact native export in every phase', () => {
  const original = mixedScenario();
  original.fixed.tcp = {
    backendHalfOpenIdleTimeoutNanoseconds: 123,
    backendEstablishedIdleTimeoutNanoseconds: 456,
    rto: { initialTimeoutNanoseconds: 789 },
    close: { mslNanoseconds: 1_000_000 },
  };
  const before = structuredClone(original);
  const timers = [
    ['lbHalfOpenIdleTimeoutMs', 'halfOpenIdleTimeoutNanoseconds'],
    ['lbEstablishedIdleTimeoutMs', 'establishedIdleTimeoutNanoseconds'],
    ['lbClosingIdleTimeoutMs', 'closingIdleTimeoutNanoseconds'],
    ['lbResetIdleTimeoutMs', 'resetIdleTimeoutNanoseconds'],
  ] as const;

  for (const [field, nativeField] of timers) {
    const disabled = applyScenarioField(original, field, '0');
    const disabledRoundTrip = JSON.parse(serializeScenario(disabled));
    assert.equal(disabledRoundTrip.states['0'].resources.loadBalancer.mappingTimeouts[nativeField], 0);
    assert.equal(scenarioFormValues(disabledRoundTrip)[field], '0');

    const exact = applyScenarioField(disabledRoundTrip, field, '9007199254.740993');
    const restored = JSON.parse(serializeScenario(exact));
    const expected = structuredClone(original);
    expected.states['0'].resources.loadBalancer.mappingTimeouts[nativeField] = '9007199254740993';
    assert.deepEqual(restored, JSON.parse(serializeScenario(expected)));
    assert.equal(scenarioFormValues(restored)[field], '9007199254.740993');
    for (const [, omittedField] of timers) {
      if (omittedField !== nativeField) {
        assert.equal(Object.hasOwn(restored.states['0'].resources.loadBalancer.mappingTimeouts, omittedField), false);
      }
    }
    assert.equal(validateScenarioField(field, '0'), undefined);
    assert.equal(validateScenarioField(field, '0.000001'), undefined);
    assert.equal(validateScenarioField(field, '9223372036854.775807'), undefined);
    for (const invalid of ['-1', '0.0000001', '9223372036854.775808', '']) {
      assert.ok(validateScenarioField(field, invalid), `${field}=${invalid}`);
      assert.throws(() => applyScenarioField(original, field, invalid));
    }
  }
  assert.deepEqual(original, before);
  const section = scenarioFieldSections.find((entry) => entry.id === 'load-balancer');
  assert.deepEqual(section?.fields.slice(-4), timers.map(([field]) => field));
});

test('backend timer edits retain exact values and do not fill unrelated defaults', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  // Disable handshake cleanup while retaining a large exact idle timeout.
  let edited = applyScenarioField(original, 'backendHalfOpenIdleTimeoutMs', '0');
  edited = applyScenarioField(edited, 'backendEstablishedIdleTimeoutMs', '9007199254.740993');
  const restored = JSON.parse(JSON.stringify(edited));

  assert.deepEqual(restored.fixed.tcp, {
    backendHalfOpenIdleTimeoutNanoseconds: 0,
    backendEstablishedIdleTimeoutNanoseconds: '9007199254740993',
  });
  assert.equal(scenarioFormValues(restored).backendHalfOpenIdleTimeoutMs, '0');
  assert.equal(scenarioFormValues(restored).backendEstablishedIdleTimeoutMs, '9007199254.740993');
  assert.deepEqual(original, before);

  for (const field of ['backendHalfOpenIdleTimeoutMs', 'backendEstablishedIdleTimeoutMs'] as const) {
    assert.equal(validateScenarioField(field, '0'), undefined);
    assert.equal(validateScenarioField(field, '0.000001'), undefined);
    assert.ok(validateScenarioField(field, '-1'));
    assert.ok(validateScenarioField(field, '0.0000001'));
  }
});

test('closing fields display backend defaults and preserve exact edits through native export', () => {
  const scenario = mixedScenario();
  scenario.fixed.tcp = {
    close: {
      mslNanoseconds: 2_500_000,
      finWaitTimeoutNanoseconds: 7_500_000,
      maxFINRetries: 2,
    },
  };
  const before = structuredClone(scenario);
  const fields = scenarioFormValues(scenario);
  assert.equal(fields.tcpMSLMs, '2.5');
  assert.equal(fields.tcpFinWaitTimeoutMs, '7.5');
  assert.equal(fields.tcpMaxFINRetries, '2');

  let edited = applyScenarioField(scenario, 'tcpMSLMs', '4611686018427.387903');
  edited = applyScenarioField(edited, 'tcpFinWaitTimeoutMs', '9007199254.740993');
  edited = applyScenarioField(edited, 'tcpMaxFINRetries', '0');
  const restored = JSON.parse(JSON.stringify(edited));
  assert.deepEqual(restored.fixed.tcp.close, {
    mslNanoseconds: '4611686018427387903',
    finWaitTimeoutNanoseconds: '9007199254740993',
    maxFINRetries: 0,
  });
  assert.equal(scenarioFormValues(restored).tcpMSLMs, '4611686018427.387903');
  assert.equal(scenarioFormValues(restored).tcpFinWaitTimeoutMs, '9007199254.740993');
  assert.equal(scenarioFormValues(restored).tcpMaxFINRetries, '0');
  assert.deepEqual(edited.fixed.network, scenario.fixed.network);
  assert.deepEqual(edited.states['0'].workload, scenario.states['0'].workload);
  assert.deepEqual(scenario, before);

  const section = scenarioFieldSections.find((entry) => entry.id === 'tcp-closing');
  assert.deepEqual(section?.fields, ['tcpMSLMs', 'tcpFinWaitTimeoutMs', 'tcpMaxFINRetries']);
});

test('closing edits keep older document omissions and reject invalid retention and retry limits', () => {
  const scenario = mixedScenario();
  const edited = applyScenarioField(scenario, 'tcpMaxFINRetries', '0');
  assert.deepEqual(edited.fixed.tcp, { close: { maxFINRetries: 0 } });
  assert.deepEqual(scenario.fixed.tcp, {});
  const valid: [ScenarioField, string][] = [
    ['tcpMSLMs', '0.000001'], ['tcpMSLMs', '4611686018427.387903'],
    ['tcpFinWaitTimeoutMs', '0.000001'], ['tcpFinWaitTimeoutMs', '9223372036854.775807'],
    ['tcpMaxFINRetries', '0'], ['tcpMaxFINRetries', '30'],
  ];
  for (const [field, raw] of valid) {
    assert.equal(validateScenarioField(field, raw), undefined, `${field}=${raw}`);
  }
  const invalid: [ScenarioField, string][] = [
    ['tcpMSLMs', '0'], ['tcpMSLMs', '4611686018427.387904'],
    ['tcpMSLMs', '0.0000001'], ['tcpFinWaitTimeoutMs', '0'],
    ['tcpFinWaitTimeoutMs', '-1'], ['tcpFinWaitTimeoutMs', '9223372036854.775808'],
    ['tcpMaxFINRetries', '-1'], ['tcpMaxFINRetries', '31'], ['tcpMaxFINRetries', '1.5'],
  ];
  for (const [field, raw] of invalid) {
    assert.ok(validateScenarioField(field, raw), `${field}=${raw}`);
    assert.throws(() => applyScenarioField(scenario, field, raw));
  }
});

test('older scenarios display Reno defaults without changing the native document', () => {
  const scenario = mixedScenario();
  const before = structuredClone(scenario);

  const fields = scenarioFormValues(scenario);
  assert.equal(fields.congestionControl, 'reno');
  assert.equal(fields.fastRetransmit, 'true');
  assert.equal(fields.cubicFastConvergence, 'true');
  assert.equal(fields.initialCongestionWindowSegments, '4');
  assert.equal(fields.initialSlowStartThresholdBytes, '65535');
  assert.deepEqual(scenario, before);
});

test('congestion edits retain mode none and large exact integers without flattening topology', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  let edited = applyScenarioField(original, 'congestionControl', 'none');
  edited = applyScenarioField(edited, 'initialCongestionWindowSegments', '2');
  edited = applyScenarioField(edited, 'initialSlowStartThresholdBytes', '9007199254740993');

  assert.deepEqual(edited.fixed.tcp?.congestionControl, {
    mode: 'none',
    fastRetransmit: false,
    initialWindowSegments: 2,
    initialSlowStartThresholdBytes: '9007199254740993',
  });
  assert.deepEqual(edited.fixed.network, original.fixed.network);
  assert.deepEqual(edited.states['0'].workload, original.states['0'].workload);
  assert.deepEqual(edited.states['0'].resources, original.states['0'].resources);
  assert.deepEqual(original, before);

  // Import/export keeps a threshold one byte above JavaScript's exact integer limit.
  const restored = JSON.parse(JSON.stringify(edited));
  const fields = scenarioFormValues(restored);
  assert.equal(fields.congestionControl, 'none');
  assert.equal(fields.initialCongestionWindowSegments, '2');
  assert.equal(fields.initialSlowStartThresholdBytes, '9007199254740993');
});

test('fast recovery follows Reno defaults and preserves an explicit disabled setting', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  // An older document displays the enabled Reno policy without writing defaults.
  assert.equal(scenarioFormValues(original).fastRetransmit, 'true');
  assert.deepEqual(original, before);

  const disabled = applyScenarioField(original, 'fastRetransmit', 'false');
  const restored = JSON.parse(JSON.stringify(disabled));
  assert.equal(restored.fixed.tcp.congestionControl.fastRetransmit, false);
  assert.equal(scenarioFormValues(restored).fastRetransmit, 'false');

  // Switching off Reno disables its dependent policy; turning Reno back on keeps false.
  const none = applyScenarioField(original, 'congestionControl', 'none');
  assert.equal(scenarioFormValues(none).fastRetransmit, 'false');
  assert.throws(
    () => applyScenarioField(none, 'fastRetransmit', 'true'),
    /requires Reno, NewReno, Tahoe or CUBIC/,
  );
  const reno = applyScenarioField(none, 'congestionControl', 'reno');
  assert.equal(scenarioFormValues(reno).fastRetransmit, 'false');
  const enabled = applyScenarioField(reno, 'fastRetransmit', 'true');
  assert.equal(enabled.fixed.tcp?.congestionControl?.fastRetransmit, true);
  assert.deepEqual(enabled.fixed.network, original.fixed.network);
  assert.deepEqual(original, before);

  // Imported rwnd-only scenarios inherit false even when the field is absent.
  const imported = structuredClone(original);
  imported.fixed.tcp = { congestionControl: { mode: 'none' } };
  assert.equal(scenarioFormValues(imported).fastRetransmit, 'false');
  assert.ok(validateScenarioField('fastRetransmit', '1'));
});

for (const mode of ['tahoe', 'newReno']) {
  test(`${mode} edits inherit early retransmit and preserve an explicit disabled choice`, () => {
    const original = mixedScenario();
    const before = structuredClone(original);

    // Selecting a mode inherits early retry without writing an extra default.
    const selected = applyScenarioField(original, 'congestionControl', mode);
    assert.equal(scenarioFormValues(selected).congestionControl, mode);
    assert.equal(scenarioFormValues(selected).fastRetransmit, 'true');
    assert.equal(selected.fixed.tcp?.congestionControl?.fastRetransmit, undefined);

    // Disabling and re-enabling fast retransmit survives native JSON export/import.
    const disabled = applyScenarioField(selected, 'fastRetransmit', 'false');
    const restored = JSON.parse(JSON.stringify(disabled));
    assert.equal(scenarioFormValues(restored).fastRetransmit, 'false');
    const enabled = applyScenarioField(restored, 'fastRetransmit', 'true');
    assert.equal(enabled.fixed.tcp?.congestionControl?.mode, mode);
    assert.equal(enabled.fixed.tcp?.congestionControl?.fastRetransmit, true);

    // Switching from none retains its explicit false until the user enables it.
    const none = applyScenarioField(original, 'congestionControl', 'none');
    const fromNone = applyScenarioField(none, 'congestionControl', mode);
    assert.equal(scenarioFormValues(fromNone).fastRetransmit, 'false');
    assert.deepEqual(enabled.fixed.network, restored.fixed.network);
    assert.deepEqual(enabled.states['0'].workload, restored.states['0'].workload);
    assert.deepEqual(original, before);
  });
}

test('CUBIC edits preserve its convergence setting across native JSON and mode changes', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  const cubic = applyScenarioField(original, 'congestionControl', 'cubic');
  assert.equal(scenarioFormValues(cubic).fastRetransmit, 'true');
  assert.equal(scenarioFormValues(cubic).cubicFastConvergence, 'true');
  assert.equal(cubic.fixed.tcp?.congestionControl?.cubicFastConvergence, undefined);

  // A single-flow experiment keeps this explicit false through export/import.
  const disabled = applyScenarioField(cubic, 'cubicFastConvergence', 'false');
  assert.deepEqual(disabled.fixed.network, original.fixed.network);
  assert.deepEqual(disabled.states['0'].workload, original.states['0'].workload);
  const imported = JSON.parse(JSON.stringify(disabled));
  assert.equal(imported.fixed.tcp.congestionControl.cubicFastConvergence, false);
  const reno = applyScenarioField(imported, 'congestionControl', 'reno');
  const selectedAgain = applyScenarioField(reno, 'congestionControl', 'cubic');
  assert.equal(scenarioFormValues(selectedAgain).cubicFastConvergence, 'false');
  assert.equal(scenarioFormValues(selectedAgain).congestionControl, 'cubic');
  assert.deepEqual(selectedAgain.fixed.network, imported.fixed.network);
  assert.deepEqual(selectedAgain.states['0'].workload, imported.states['0'].workload);
  assert.deepEqual(original, before);
  assert.ok(validateScenarioField('cubicFastConvergence', '1'));
});

test('congestion validation keeps segment multiplication and threshold byte counts in range', () => {
  assert.deepEqual(scenarioSelectFields.congestionControl.options, [
    { value: 'reno', label: 'Reno' },
    { value: 'newReno', label: 'NewReno' },
    { value: 'tahoe', label: 'Tahoe' },
    { value: 'cubic', label: 'CUBIC' },
    { value: 'none', label: 'None (receive window only)' },
  ]);
  const invalid: [ScenarioField, string][] = [
    ['congestionControl', 'bbr'], ['congestionControl', ''],
    ['initialCongestionWindowSegments', '0'], ['initialCongestionWindowSegments', '1.5'],
    ['initialCongestionWindowSegments', '9007199254740992'],
    ['initialSlowStartThresholdBytes', '0'], ['initialSlowStartThresholdBytes', '2047'],
    ['initialSlowStartThresholdBytes', '9223372036854775808'],
  ];
  for (const [name, raw] of invalid) {
    assert.ok(validateScenarioField(name, raw), `${name}=${raw} must fail`);
  }

  const valid: [ScenarioField, string][] = [
    ['congestionControl', 'none'], ['congestionControl', 'reno'],
    ['congestionControl', 'newReno'],
    ['initialCongestionWindowSegments', '1'],
    ['initialCongestionWindowSegments', '9007199254740991'],
    ['initialSlowStartThresholdBytes', '2048'],
    ['initialSlowStartThresholdBytes', '9223372036854775807'],
  ];
  for (const [name, raw] of valid) {
    assert.equal(validateScenarioField(name, raw), undefined, `${name}=${raw} must pass`);
  }
});

test('timer edits preserve explicit false, disabled idle cleanup and unrelated topology', () => {
  const original = mixedScenario();
  const before = structuredClone(original);

  // Fixed retry timing and backoff are separate choices; zero disables only idle cleanup.
  let edited = applyScenarioField(original, 'rtoMode', 'fixed');
  edited = applyScenarioField(edited, 'rtoBackoff', 'false');
  edited = applyScenarioField(edited, 'rtoMinimumMs', '100');
  edited = applyScenarioField(edited, 'rtoInitialMs', '200.000001');
  edited = applyScenarioField(edited, 'rtoMaximumMs', '2000');
  edited = applyScenarioField(edited, 'clientIdleTimeoutMs', '0');
  edited = applyScenarioField(edited, 'lbHalfOpenIdleTimeoutMs', '0');
  edited = applyScenarioField(edited, 'lbEstablishedIdleTimeoutMs', '40000');

  assert.deepEqual(edited.fixed.tcp, {
    rto: {
      mode: 'fixed',
      exponentialBackoff: false,
      minimumTimeoutNanoseconds: 100_000_000,
      initialTimeoutNanoseconds: 200_000_001,
      maximumTimeoutNanoseconds: 2_000_000_000,
    },
    clientIdleTimeoutNanoseconds: 0,
  });
  assert.deepEqual(edited.fixed.network, original.fixed.network);
  assert.deepEqual(edited.states['0'].workload, original.states['0'].workload);
  assert.deepEqual(edited.states['0'].resources.backends, original.states['0'].resources.backends);
  assert.equal(edited.states['0'].resources.loadBalancer.mappingTimeouts.halfOpenIdleTimeoutNanoseconds, 0);
  assert.equal(edited.states['0'].resources.loadBalancer.mappingTimeouts.establishedIdleTimeoutNanoseconds, 40_000_000_000);
  assert.deepEqual(original, before);

  // Import/export keeps exact nanoseconds as well as the disabled settings.
  const restored = JSON.parse(JSON.stringify(edited));
  const fields = scenarioFormValues(restored);
  assert.equal(fields.rtoBackoff, 'false');
  assert.equal(fields.rtoInitialMs, '200.000001');
  assert.equal(fields.clientIdleTimeoutMs, '0');
  assert.equal(fields.lbHalfOpenIdleTimeoutMs, '0');
});

test('timer fields reject invalid modes, nonpositive RTO and negative idle delays', () => {
  const invalid: [ScenarioField, string][] = [
    ['rtoMode', 'fast'], ['rtoMode', ''], ['rtoBackoff', '0'],
    ['rtoInitialMs', '0'], ['rtoMinimumMs', '-1'], ['rtoMaximumMs', '0'],
    ['rtoInitialMs', '0.0000001'], ['clientIdleTimeoutMs', '-1'],
    ['lbHalfOpenIdleTimeoutMs', '-1'], ['lbEstablishedIdleTimeoutMs', 'Infinity'],
  ];
  for (const [name, raw] of invalid) {
    assert.ok(validateScenarioField(name, raw), `${name}=${raw} must fail`);
  }

  const valid: [ScenarioField, string][] = [
    ['rtoMode', 'adaptive'], ['rtoMode', 'fixed'], ['rtoBackoff', 'false'],
    ['rtoInitialMs', '0.000001'], ['clientIdleTimeoutMs', '0'],
    ['lbHalfOpenIdleTimeoutMs', '0'], ['lbEstablishedIdleTimeoutMs', '0'],
  ];
  for (const [name, raw] of valid) {
    assert.equal(validateScenarioField(name, raw), undefined, `${name}=${raw} must pass`);
  }
});

test('presets populate the same form while one manual edit keeps their native structure', () => {
  const defaults = scenarioFixture();
  const originalIds = new Set([
    'mixed-clients',
    'mixed-backends',
    'ingress-bottleneck',
    'egress-bottleneck',
    'simulator-throughput',
  ]);
  const originalPresets = scenarioPresets.filter((preset) => originalIds.has(preset.id));
  assert.equal(originalPresets.length, 5);

  for (const preset of originalPresets) {
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
    expected.fixed.execution.speed = 2.5;
    assert.deepEqual(updated, expected);
    assert.deepEqual(scenario, before);
    assert.equal(updated.fixed.execution.seed, scenario.fixed.execution.seed);
    assert.equal(updated.fixed.execution.maxEvents, scenario.fixed.execution.maxEvents);
  }
});

test('every catalog preset projects valid fields and keeps native settings after one edit', () => {
  const defaults = scenarioFixture();

  for (const preset of scenarioPresets) {
    const scenario = preset.build(defaults);
    const before = structuredClone(scenario);
    const fields = scenarioFormValues(scenario);
    const gaps = new Set(
      scenario.states['0'].workload.clientGroups.map((group) => String(group.clientGapNanoseconds)),
    );

    for (const [name, raw] of Object.entries(fields)) {
      // A blank creation gap represents mixed group values without changing them.
      if (name === 'clientGapMs' && raw === '') {
        assert.ok(gaps.size > 1, `${preset.id}: mixed creation gaps`);
        continue;
      }

      const error = validateScenarioField(name as ScenarioField, raw);
      assert.equal(error, undefined, `${preset.id}: ${name}=${raw}`);
    }

    const count = scenario.states['0'].workload.clientGroups.reduce(
      (total, group) => total + BigInt(group.count),
      0n,
    );
    assert.equal(fields.clients, String(count), `${preset.id}: total clients`);
    assert.equal(
      fields.servers,
      String(scenario.states['0'].resources.backends.length),
      `${preset.id}: backend count`,
    );

    const edited = applyScenarioField(scenario, 'speed', '2.5');
    const expected = structuredClone(scenario);
    expected.fixed.execution.speed = 2.5;

    assert.deepEqual(edited, expected, `${preset.id}: one field edit`);
    assert.deepEqual(scenario, before, `${preset.id}: original draft`);
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
  expected.fixed.network.defaults.bandwidthBitsPerSecond = '9007199254740993';
  expected.fixed.workload.defaults.bytes = -1;
  expected.fixed.workload.defaults.packets = 200;
  expected.states['0'].resources.loadBalancer.algorithm = 'roundRobin';
  assert.deepEqual(updated, expected);
  assert.deepEqual(scenario, before);
  assert.equal(updated.states['0'].workload.clientGroups[0].stream.bytes, 1234);
  assert.equal(updated.states['0'].workload.clientGroups[0].overrides![0].stream.bytes, 0);

  const ingress = applyScenarioField(scenario, 'ingressQueueCapacity', '0');
  assert.equal(ingress.states['0'].resources.loadBalancer.ingress.queueCapacity, 0);
  assert.equal(ingress.fixed.network.defaults.queueCapacity, scenario.fixed.network.defaults.queueCapacity);
  assert.deepEqual(ingress.states['0'].workload.clientGroups, scenario.states['0'].workload.clientGroups);
});

test('time edits use exact decimal arithmetic and retain full Go integer limits', () => {
  let scenario = scenarioFixture();
  scenario = applyScenarioField(scenario, 'durationSeconds', '9223372036.854775807');
  scenario = applyScenarioField(scenario, 'snapshotIntervalMs', '0.000001');
  scenario = applyScenarioField(scenario, 'packetGapMs', '9007199254.740993');
  scenario = applyScenarioField(scenario, 'ingressPacketTravelTimeMs', '9223372036854.775807');
  scenario = applyScenarioField(scenario, 'seed', '-9223372036854775808');
  scenario = applyScenarioField(scenario, 'maxEvents', '18446744073709551615');
  assert.equal(scenario.fixed.execution.durationNanoseconds, '9223372036854775807');
  assert.equal(scenario.fixed.execution.snapshotIntervalNanoseconds, 1);
  assert.equal(scenario.fixed.workload.defaults.packetGapNanoseconds, '9007199254740993');
  assert.equal(scenario.states['0'].resources.loadBalancer.ingress.packetTravelTimeNanoseconds, '9223372036854775807');
  assert.equal(scenario.fixed.execution.seed, '-9223372036854775808');
  assert.equal(scenario.fixed.execution.maxEvents, '18446744073709551615');

  const fields = scenarioFormValues(scenario);
  assert.equal(fields.durationSeconds, '9223372036.854775807');
  assert.equal(fields.snapshotIntervalMs, '0.000001');
  assert.equal(fields.packetGapMs, '9007199254.740993');
});

test('client totals scale existing groups deterministically without flattening settings', () => {
  const scenario = mixedScenario();
  const before = structuredClone(scenario);
  const updated = applyScenarioField(scenario, 'clients', '17');
  assert.deepEqual(updated.states['0'].workload.clientGroups.map((group) => group.count), [9, 8]);
  const expected = structuredClone(scenario);
  expected.states['0'].workload.clientGroups[0].count = 9;
  assert.deepEqual(updated, expected);
  assert.deepEqual(scenario, before);
  assert.deepEqual(applyScenarioField(scenario, 'clients', '17'), updated);

  scenario.states['0'].workload.clientGroups[0].count = 12;
  scenario.states['0'].workload.clientGroups[1].count = 4;
  const proportional = applyScenarioField(scenario, 'clients', '8');
  assert.deepEqual(proportional.states['0'].workload.clientGroups.map((group) => group.count), [6, 2]);

  const empty = structuredClone(scenario);
  for (const group of empty.states['0'].workload.clientGroups) {
    group.count = 0;
    group.overrides = [];
  }
  const expanded = applyScenarioField(empty, 'clients', '3');
  assert.deepEqual(expanded.states['0'].workload.clientGroups.map((group) => group.count), [2, 1]);
  assert.deepEqual(expanded.fixed.network.profiles, empty.fixed.network.profiles);
});

test('count edits applied from the same baseline retain group ratios and custom backends', () => {
  const baseline = mixedScenario();
  baseline.states['0'].workload.clientGroups[0].overrides = [];
  baseline.states['0'].resources.backends[1].name = 'Keep the second backend';
  baseline.states['0'].resources.backends[1].network.fromLoadBalancer.queueCapacity = 19;
  const before = structuredClone(baseline);

  const intermediateClients = applyScenarioField(baseline, 'clients', '1');
  assert.deepEqual(intermediateClients.states['0'].workload.clientGroups.map((group) => group.count), [1, 0]);
  const finalClients = applyScenarioField(baseline, 'clients', '18');
  assert.deepEqual(finalClients.states['0'].workload.clientGroups.map((group) => group.count), [9, 9]);

  const intermediateServers = applyScenarioField(baseline, 'servers', '1');
  assert.equal(intermediateServers.states['0'].resources.backends.length, 1);
  const finalServers = applyScenarioField(baseline, 'servers', '4');
  assert.deepEqual(finalServers.states['0'].resources.backends.slice(0, 2), baseline.states['0'].resources.backends);
  assert.deepEqual(baseline, before);
});

test('client resize refuses to erase explicit overrides and mixed gap edits apply to every group', () => {
  const scenario = mixedScenario();
  scenario.states['0'].workload.clientGroups[1].clientGapNanoseconds = 20;
  const before = structuredClone(scenario);
  assert.throws(
    () => applyScenarioField(scenario, 'clients', '4'),
    /slow-clients.*at least 4 clients.*explicit overrides/,
  );
  assert.deepEqual(scenario, before);
  assert.equal(scenarioFormValues(scenario).clientGapMs, '');

  const updated = applyScenarioField(scenario, 'clientGapMs', '0.000001');
  assert.deepEqual(updated.states['0'].workload.clientGroups.map((group) => group.clientGapNanoseconds), [1, 1]);
  const expected = structuredClone(scenario);
  for (const group of expected.states['0'].workload.clientGroups) group.clientGapNanoseconds = 1;
  assert.deepEqual(updated, expected);
});

test('an empty workload gets a unique group and retains a manually chosen creation gap', () => {
  const scenario = scenarioFixture();
  scenario.fixed.loadBalancer.id = 'clients';
  scenario.states['0'].resources.backends = [{
    id: 'clients-1', address: '10.0.0.1', port: 80,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  }];
  const before = structuredClone(scenario);
  const withGap = applyScenarioField(scenario, 'clientGapMs', '0.000001');
  const updated = applyScenarioField(withGap, 'clients', '2');
  assert.equal(updated.states['0'].workload.clientGroups.length, 1);
  const group = updated.states['0'].workload.clientGroups[0];
  assert.equal(group.id, 'clients-2');
  assert.equal(group.count, 2);
  assert.equal(group.clientGapNanoseconds, 1);
  assert.deepEqual(group.network, { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} });
  assert.deepEqual(group.stream, {});
  assert.deepEqual(scenario, before);
});

test('backend resizing preserves configured prefixes and avoids resource and endpoint collisions', () => {
  const scenario = mixedScenario();
  scenario.fixed.loadBalancer.id = 'backend-1';
  scenario.states['0'].workload.clientGroups[0].id = 'backend-2';
  scenario.states['0'].resources.backends[0] = {
    id: 'original', name: 'Keep this', address: '::ffff:a00:1', port: '80',
    network: { profile: 'slow-client', toLoadBalancer: { lossRate: 0.3 }, fromLoadBalancer: {} },
  };
  scenario.states['0'].resources.backends[1].address = '10.0.0.2';
  scenario.states['0'].resources.backends[1].id = 'another-original';
  const before = structuredClone(scenario);

  const updated = applyScenarioField(scenario, 'servers', '4');
  assert.deepEqual(updated.states['0'].resources.backends.slice(0, 2), scenario.states['0'].resources.backends);
  assert.deepEqual(updated.states['0'].resources.backends.slice(2).map((backend) => [backend.id, backend.address, backend.port]), [
    ['backend-3', '10.0.0.3', 80],
    ['backend-4', '10.0.0.4', 80],
  ]);
  for (const backend of updated.states['0'].resources.backends.slice(2)) {
    assert.deepEqual(backend.network, { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} });
  }
  assert.deepEqual(updated.states['0'].workload, scenario.states['0'].workload);
  assert.deepEqual(updated.fixed.network, scenario.fixed.network);
  assert.deepEqual(scenario, before);

  const trimmed = applyScenarioField(updated, 'servers', '1');
  assert.deepEqual(trimmed.states['0'].resources.backends, [scenario.states['0'].resources.backends[0]]);
  const empty = applyScenarioField(updated, 'servers', '0');
  assert.deepEqual(empty.states['0'].resources.backends, []);

  const many = applyScenarioField(scenarioFixture(), 'servers', '300');
  assert.equal(many.states['0'].resources.backends[253].address, '10.0.0.254');
  assert.equal(many.states['0'].resources.backends[254].address, '10.0.1.1');
  assert.equal(new Set(many.states['0'].resources.backends.map((backend) => backend.address)).size, 300);
});

test('field validation rejects overflow, subnanosecond values and malformed integers', () => {
  const valid: [ScenarioField, string][] = [
    ['receiveBufferBytes', '1'], ['receiveBufferBytes', '65535'],
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
    ['receiveBufferBytes', '0'], ['receiveBufferBytes', '-1'],
    ['receiveBufferBytes', '65536'], ['receiveBufferBytes', '1.5'],
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
