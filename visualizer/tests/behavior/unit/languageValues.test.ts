import assert from 'node:assert/strict';
import test from 'node:test';
import { tokenize } from '../../../src/scenario-language/lexer.ts';
import {
  bitRate, booleanValue, countValue, dataBytes, eventLimit, percentage,
  probability, seedValue, speedValue, textValue, timeValue,
} from '../../../src/scenario-language/values.ts';
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

// Times and integer controls keep every digit, including values beyond Number's range.
test('language converts times, signed seeds and unsigned event limits exactly', () => {
  assert.equal(timeValue(literal('0.5ms')), 500_000);
  assert.equal(timeValue(literal('2m')), 120_000_000_000);
  assert.equal(timeValue(literal('9007199254740993ns')), '9007199254740993');
  assert.equal(seedValue(literal('-9223372036854775808')), '-9223372036854775808');
  assert.equal(eventLimit(literal('18446744073709551615')), '18446744073709551615');
  assert.equal(countValue(literal('1.0')), 1);
  assert.throws(() => timeValue(literal('0.1ns')), /whole native unit/);
  assert.throws(() => timeValue(literal('-1ms')), /between/);
  assert.throws(() => countValue(literal('1.5')), /whole native unit/);
  assert.throws(() => seedValue(literal('9223372036854775808')), /between/);
  assert.throws(() => eventLimit(literal('18446744073709551616')), /between/);
  assert.throws(() => eventLimit(literal('0')), /between/);
});

// Percentages have two native meanings; booleans never accept truthy strings or numbers.
test('language converts floats, percentages, booleans and display names explicitly', () => {
  assert.equal(speedValue(literal('0')), 0);
  assert.equal(speedValue(literal('1.25')), 1.25);
  assert.equal(percentage(literal('80%')), 80);
  assert.equal(probability(literal('1%')), 0.01);
  assert.equal(probability(literal('100%')), 1);
  const paddedHundred = `100.${'0'.repeat(23)}`;
  assert.equal(percentage(literal(`${paddedHundred}%`)), 100);
  assert.equal(probability(literal(`${paddedHundred}%`)), 1);
  assert.equal(speedValue(literal(paddedHundred)), 100);
  assert.ok(percentage(literal('99.99999999999999999%')) <= 100);
  assert.equal(booleanValue(literal('false')), false);
  assert.equal(textValue(literal('"API #1"')), 'API #1');
  assert.throws(() => percentage(literal('101%')), /percentage/);
  assert.throws(() => percentage(literal('100.00000000000000001%')), /percentage/);
  assert.throws(() => probability(literal('100.00000000000000001%')), /percentage/);
  assert.throws(() => percentage(literal('1')), /percentage/);
  assert.throws(() => speedValue(literal('-1')), /Speed/);
  assert.throws(() => speedValue(literal('1s')), /Speed/);
  assert.throws(() => booleanValue(literal('1')), /true or false/);
  assert.throws(() => textValue(literal('unquoted')), /double-quoted/);
});
