import assert from 'node:assert/strict';
import test from 'node:test';
import { readTelemetryJSON, telemetrySource } from '../../../src/telemetry/exact.ts';
import { parseSimulationEnded } from '../../../src/telemetry/lifecycleParse.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

test('large integer tokens stay exact in the archive while live views receive numbers', () => {
  const decoded = readTelemetryJSON(`{
    "bytes": 18446744073709551615,
    "signed": -9223372036854775808,
    "safe": 9007199254740991,
    "ratio": 0.125,
    "identity": "9007199254740993"
  }`) as { bytes: number; signed: number; safe: number; ratio: number; identity: string };

  assert.equal(typeof decoded.bytes, 'number');
  assert.equal(decoded.ratio, 0.125);
  assert.deepEqual(telemetrySource(decoded), {
    bytes: '18446744073709551615', signed: '-9223372036854775808',
    safe: 9007199254740991, ratio: 0.125, identity: '9007199254740993',
  });
});

test('embedded end snapshots and topology definitions retain their own exact sources', () => {
  const finalSnapshot = telemetryFrame();
  finalSnapshot.simulation.status = 'finished';
  finalSnapshot.simulation.payload = { receivedBytes: 42, acknowledgedBytes: 42 };
  const definition = topologyDefinition();
  definition.links[0].config.bandwidthBitsPerSecond = 43;
  const json = JSON.stringify({
    type: 'simulationEnded', runId: 'run-1', reason: 'finished',
    finalSnapshot, topologyDefinition: definition,
  }).replaceAll(':42', ':18446744073709551615').replaceAll(':43', ':9007199254740993');

  const ended = parseSimulationEnded(readTelemetryJSON(json));
  assert.ok(ended.sample);
  assert.equal(telemetrySource(ended.sample.snapshot).simulation.payload?.receivedBytes,
    '18446744073709551615');
  assert.equal(telemetrySource(ended.sample.definition).links[0].config.bandwidthBitsPerSecond,
    '9007199254740993');
  assert.equal(typeof ended.sample.data.topology?.links[0].config.bandwidthBitsPerSecond, 'number');
});

test('exact telemetry rejects duplicate fields, invalid numbers and already-rounded input', () => {
  assert.throws(() => readTelemetryJSON('{"bytes":1,"bytes":2}'), /duplicate field/);
  assert.throws(() => readTelemetryJSON('{"rate":1e999}'), /non-finite/);
  assert.throws(() => telemetrySource({ bytes: Number.MAX_SAFE_INTEGER + 1 }), /unavailable/);
  assert.deepEqual(telemetrySource({ bytes: 1024 }), { bytes: 1024 });
});
