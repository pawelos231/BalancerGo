import assert from 'node:assert/strict';
import test from 'node:test';
import { closeOutcomeLabel, formatBandwidth, formatBytesPerSecond, tcpPhaseLabel, tcpStateLabel } from '../../../src/topology/format.ts';

test('measured traffic uses SI byte rates rather than configured bitrates', () => {
  assert.equal(formatBytesPerSecond(0), '0 B/s');
  assert.equal(formatBytesPerSecond(999), '999 B/s');
  assert.equal(formatBytesPerSecond(1_000), '1 kB/s');
  assert.equal(formatBytesPerSecond(1_250), '1.25 kB/s');
  assert.equal(formatBytesPerSecond(125_000_000), '125 MB/s');
  assert.equal(formatBytesPerSecond(1_000_000_000), '1 GB/s');
  assert.equal(formatBandwidth(1_000_000_000), '1 Gbit/s');
});

test('missing or invalid measured rates stay unknown rather than becoming zero', () => {
  for (const value of [undefined, NaN, Infinity, -1]) {
    assert.equal(formatBytesPerSecond(value), '—');
  }
});

test('TCP phase labels explain recovery without hiding future states', () => {
  assert.equal(tcpPhaseLabel('slowStart'), 'Slow start');
  assert.equal(tcpPhaseLabel('congestionAvoidance'), 'Congestion avoidance');
  assert.equal(tcpPhaseLabel('fastRecovery'), 'Fast recovery');
  assert.equal(tcpPhaseLabel('disabled'), 'Disabled');
  assert.equal(tcpPhaseLabel('futurePhase'), 'futurePhase');
});

test('TCP closing labels distinguish protocol state and terminal outcome', () => {
  assert.equal(tcpStateLabel('finWait1'), 'FIN_WAIT_1');
  assert.equal(tcpStateLabel('finWait2'), 'FIN_WAIT_2');
  assert.equal(tcpStateLabel('lastAck'), 'LAST_ACK');
  assert.equal(tcpStateLabel('timeWait'), 'TIME_WAIT');
  assert.equal(tcpStateLabel('futureState'), 'futureState');
  assert.equal(closeOutcomeLabel('graceful'), 'Closed gracefully');
  assert.equal(closeOutcomeLabel('finRetryLimit'), 'FIN retry limit reached');
  assert.equal(closeOutcomeLabel('reset'), 'Connection reset');
  assert.equal(closeOutcomeLabel('finWaitTimeout'), 'FIN wait timeout');
  assert.equal(closeOutcomeLabel('aborted'), 'Aborted');
  assert.equal(closeOutcomeLabel('futureOutcome'), 'futureOutcome');
});
