import assert from 'node:assert/strict';
import test from 'node:test';
import { tokenize } from '../../../src/scenario-language/lexer.ts';
import { bitRate, dataBytes } from '../../../src/scenario-language/values.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

const literal = (source: string) => tokenize(source)[0];

// The same suffix has different native multipliers for data and a link rate.
test('language byte and bitrate quantities preserve exact decimal meaning', () => {
  assert.equal(dataBytes(literal('1460B')), 1460);
  assert.equal(dataBytes(literal('1.5KB')), 1500);
  assert.equal(dataBytes(literal('0.000000001GB')), 1);
  assert.equal(bitRate(literal('1GB')), 8_000_000_000);
  assert.equal(bitRate(literal('0.125B')), 1);
  assert.equal(dataBytes(literal('1EB')), '1000000000000000000');
  assert.equal(bitRate(literal('1EB')), '8000000000000000000');
  assert.equal(dataBytes(literal('2EB')), '2000000000000000000');
  assert.equal(dataBytes(literal('9007199254740993B')), '9007199254740993');
});

// Overflow is field-specific: 2EB of data fits, but its bit/s equivalent does not.
test('language quantities reject unsupported units, fractions and out-of-range values', () => {
  assert.throws(() => bitRate(literal('2EB')), /between/);
  assert.throws(() => bitRate(literal('0B')), /between/);
  assert.throws(() => dataBytes(literal('-1B')), /between/);
  assert.throws(() => dataBytes(literal('0.1B')), /whole native unit/);
  assert.throws(() => bitRate(literal('0.1B')), /whole native unit/);
  for (const value of ['1GiB', '1Mbps', '1Gbps', '1constructor', '1']) {
    assert.throws(() => bitRate(literal(value)), /Unsupported unit/);
  }
});

// A bounded literal protects BigInt conversion; errors keep the original token location.
test('language quantity errors preserve source locations and bound large literals', () => {
  assert.throws(() => dataBytes(tokenize('mss\n  0.1B')[2]), error => {
    assert.ok(error instanceof LanguageError);
    assert.equal(error.range.start.line, 2);
    assert.equal(error.range.start.column, 3);
    return true;
  });
  assert.throws(() => bitRate(literal(`${'1'.repeat(129)}B`)), /128 characters/);
});
