import assert from 'node:assert/strict';
import test from 'node:test';
import { formatBandwidth, formatBytesPerSecond } from '../src/topology/format.ts';

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
