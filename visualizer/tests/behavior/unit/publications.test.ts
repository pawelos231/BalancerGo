import assert from 'node:assert/strict';
import test from 'node:test';
import { parseSnapshot } from '../../../src/telemetry/parse.ts';
import { PublicationTracker } from '../../../src/telemetry/publications.ts';
import { idleFrame, telemetryFrame } from '../../telemetryFixture.ts';

function publication(sequence: string, runId = 'run-1') {
  return telemetryFrame('1', runId, 1, sequence).simulation;
}

test('publication sequence remains exact decimal text across the full positive uint64 range', () => {
  for (const status of ['running', 'finished', 'failed'] as const) {
    for (const sequence of ['1', '9007199254740991', '9007199254740992', '9007199254740993', '18446744073709551615']) {
      const frame = telemetryFrame('1', 'run-1', 1, sequence);
      frame.simulation.status = status;
      const parsed = parseSnapshot(JSON.parse(JSON.stringify(frame)));
      assert.equal(parsed.simulation.publicationSequence, sequence);
    }
  }
});

test('wire validation rejects missing, numeric, noncanonical and overflowing publication sequences', () => {
  const frame = telemetryFrame();
  const invalid = [
    undefined, null, true, 1, 0, -1, 1.5, Infinity, 9007199254740992,
    '', '0', '00', '01', '+1', '-1', '1.0', '1e3', ' 1', '1 ', '1\n', '1\r\n',
    '١', '18446744073709551616', '99999999999999999999999999999999999999',
  ];
  for (const status of ['running', 'finished', 'failed'] as const) {
    for (const publicationSequence of invalid) {
      assert.throws(() => parseSnapshot({
        ...frame, simulation: { ...frame.simulation, status, publicationSequence },
      }), /incomplete/, `${status}: ${String(publicationSequence)}`);
    }
  }
  const { publicationSequence: omitted, ...withoutSequence } = frame.simulation;
  assert.equal(omitted, '1');
  assert.throws(() => parseSnapshot({ ...frame, simulation: withoutSequence }), /incomplete/);
});

test('idle omits publication identity and rejects even an undefined sequence property', () => {
  const frame = idleFrame();
  assert.equal(parseSnapshot(frame).simulation.publicationSequence, undefined);
  for (const publicationSequence of [undefined, null, '1', 1]) {
    assert.throws(() => parseSnapshot({
      ...frame, simulation: { ...frame.simulation, publicationSequence },
    }), /incomplete/);
  }
});

test('first attachment establishes a baseline without counting earlier publications as missing', () => {
  const tracker = new PublicationTracker();
  const first = tracker.observe(publication('7'));
  assert.equal(first.kind, 'baseline');
  assert.equal(first.firstObservedSequence, '7');
  assert.equal(first.highestSeenSequence, '7');
  assert.equal(first.missingSinceLast, '0');
  assert.equal(first.missingTotal, '0');
  assert.equal(first.newPublication, true);
  assert.equal(first.acceptSnapshot, true);
});

test('gaps count only missing publications after the baseline and accumulate exactly', () => {
  const tracker = new PublicationTracker();
  tracker.observe(publication('2'));
  const gap = tracker.observe(publication('5'));
  assert.equal(gap.kind, 'gap');
  assert.equal(gap.missingSinceLast, '2'); // 3 and 4
  assert.equal(gap.missingTotal, '2');
  assert.equal(tracker.observe(publication('6')).kind, 'contiguous');
  const next = tracker.observe(publication('9'));
  assert.equal(next.firstObservedSequence, '2');
  assert.equal(next.missingSinceLast, '2');
  assert.equal(next.missingTotal, '4');
});

test('duplicate and stale publications never advance the watermark or become measurements', () => {
  const tracker = new PublicationTracker();
  tracker.observe(publication('2'));
  tracker.observe(publication('5'));
  for (const [sequence, kind] of [['5', 'duplicate'], ['4', 'stale'], ['1', 'stale']]) {
    const observation = tracker.observe(publication(sequence));
    assert.equal(observation.kind, kind);
    assert.equal(observation.newPublication, false);
    assert.equal(observation.acceptSnapshot, false);
    assert.equal(observation.highestSeenSequence, '5');
    assert.equal(observation.missingSinceLast, '0');
    assert.equal(observation.missingTotal, '2');
  }
  assert.equal(tracker.observe(publication('6')).kind, 'contiguous');
});

test('reconnect preserves coverage and allows only one cached duplicate to bootstrap the view', () => {
  const tracker = new PublicationTracker();
  tracker.observe(publication('2'));
  tracker.observe(publication('5'));
  tracker.reconnect();
  assert.equal(tracker.observe(publication('4')).acceptSnapshot, false);
  const cached = tracker.observe(publication('5'));
  assert.equal(cached.kind, 'duplicate');
  assert.equal(cached.newPublication, false);
  assert.equal(cached.acceptSnapshot, true);
  assert.equal(cached.firstObservedSequence, '2');
  assert.equal(cached.missingTotal, '2');
  assert.equal(tracker.observe(publication('5')).acceptSnapshot, false);
  tracker.reconnect();
  assert.equal(tracker.observe(publication('7')).missingSinceLast, '1');
  assert.equal(tracker.observe(publication('7')).acceptSnapshot, false);
});

test('new runs and idle reset the bounded observation scope', () => {
  const tracker = new PublicationTracker();
  tracker.observe(publication('2'));
  tracker.observe(publication('5'));
  const nextRun = tracker.observe(publication('12', 'run-2'));
  assert.equal(nextRun.kind, 'baseline');
  assert.equal(nextRun.runId, 'run-2');
  assert.equal(nextRun.firstObservedSequence, '12');
  assert.equal(nextRun.missingTotal, '0');
  const idle = tracker.observe(idleFrame().simulation);
  assert.equal(idle.kind, 'idle');
  assert.equal(idle.runId, null);
  assert.equal(idle.highestSeenSequence, null);
  assert.equal(idle.newPublication, false);
  assert.equal(idle.acceptSnapshot, true);
  assert.equal(tracker.observe(publication('1', 'run-2')).kind, 'baseline');
});

test('ordering and gap totals remain lossless beyond the safe integer range', () => {
  const tracker = new PublicationTracker();
  tracker.observe(publication('1'));
  const largeGap = tracker.observe(publication('18446744073709551614'));
  assert.equal(largeGap.missingSinceLast, '18446744073709551612');
  assert.equal(largeGap.missingTotal, '18446744073709551612');
  const maximum = tracker.observe(publication('18446744073709551615'));
  assert.equal(maximum.kind, 'contiguous');
  assert.equal(maximum.highestSeenSequence, '18446744073709551615');
  assert.equal(tracker.observe(publication('18446744073709551614')).kind, 'stale');
});
