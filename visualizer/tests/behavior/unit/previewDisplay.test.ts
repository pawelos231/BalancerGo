import assert from 'node:assert/strict';
import test from 'node:test';
import {
  continuousValueTicks, createPreviewDisplay, densityAxis, formatChartNumber,
} from '../../../src/docs/previewDisplay.ts';

test('normal time axes display microseconds and transform reciprocal density units', () => {
  const display = createPreviewDisplay('ns', '0', '20000');
  assert.equal(display.unitLabel, 'µs');
  assert.equal(display.scale, 1000);
  assert.equal(display.quantity('20000'), '20');
  assert.equal(display.quantity('10000'), '10');
  assert.equal(display.quantity('2000'), '2');
  assert.equal(formatChartNumber(0.00022939 * display.scale), '0.2294');
  const axis = densityAxis(0.00022939 * display.scale);
  assert.equal(axis.maximum, 0.25);
  assert.deepEqual(axis.ticks, [0, 0.05, 0.1, 0.15, 0.2, 0.25]);
});

test('Student t time bounds, location and scale display in milliseconds', () => {
  const profile = { location: '10000000', scale: '2000000', degreesOfFreedom: '3', min: '0', max: '30000000' };
  const display = createPreviewDisplay('ns', profile.min, profile.max);
  assert.equal(display.unitLabel, 'ms');
  assert.equal(display.scale, 1_000_000);
  assert.equal(display.quantity(profile.location), '10');
  assert.equal(display.quantity(profile.scale), '2');
  assert.equal(display.quantity(profile.max), '30');
});

test('uniform display preserves unit area and shows terminating heights exactly', () => {
  const display = createPreviewDisplay('ns', '5000', '15000');
  assert.equal(display.unitLabel, 'µs');
  assert.deepEqual(['5000', '15000', '2500', '17500'].map(display.quantity), ['5', '15', '2.5', '17.5']);
  assert.equal(display.exactDensity('1 / 10000'), '0.1');
  assert.equal(Number(display.exactDensity('1 / 10000')) * 10, 1);
  assert.equal(createPreviewDisplay('unit', '0', '3').exactDensity('1 / 3'), '1 / 3');
  assert.equal(createPreviewDisplay('unit', '0', '2').exactDensity('1 / 2'), '0.5');
});

test('precise byte axes retain every digit and choose units from their small span', () => {
  const display = createPreviewDisplay('B', '9007199254740992.125', '9007199254740992.375');
  assert.equal(display.unitLabel, 'B');
  assert.equal(display.scale, 1);
  for (const value of ['9007199254740992.125', '9007199254740992.375', '9007199254740992.0625', '9007199254740992.4375']) {
    assert.equal(display.quantity(value), value);
  }
  assert.equal(display.exactDensity('4'), '4');
  const scaled = createPreviewDisplay('B', '0', '1000000');
  assert.equal(scaled.quantity('9007199254740992.125'), '9007199254.740992125');
});

test('signed padding, tiny spans, zero constants and large constants remain exact', () => {
  const time = createPreviewDisplay('ns', '0', '20000');
  assert.equal(time.quantity('-5000'), '-5');
  assert.equal(time.quantity('-0.125'), '-0.000125');
  const tiny = createPreviewDisplay('ns', '0.00000000000000000001', '0.00000000000000000003');
  assert.equal(tiny.unitLabel, 'ns');
  assert.equal(tiny.quantity('0.000000000000000000005'), '0.000000000000000000005');
  const zero = createPreviewDisplay('ns', '0', '0');
  assert.equal(zero.unitLabel, 'ns');
  assert.equal(zero.quantity('0'), '0');
  const constant = createPreviewDisplay('ns', '2000000000', '2000000000');
  assert.equal(constant.unitLabel, 's');
  assert.equal(constant.quantity('2000000000'), '2');
});

test('byte and bitrate units use decimal engineering scales while percentages remain unchanged', () => {
  const bytes = createPreviewDisplay('B', '0', '8000');
  assert.equal(bytes.unitLabel, 'KB');
  assert.equal(bytes.quantity('8000'), '8');
  assert.equal(bytes.exactDensity('1 / 8000'), '0.125');
  const rate = createPreviewDisplay('bit/s', '1000000000', '4000000000');
  assert.equal(rate.unitLabel, 'Gbit/s');
  assert.equal(rate.scale, 1_000_000_000);
  assert.equal(rate.quantity('1500000000'), '1.5');
  assert.equal(rate.exactDensity('1 / 3000000000'), '1 / 3');
  const percentage = createPreviewDisplay('%', '0', '100');
  assert.equal(percentage.unitLabel, '%');
  assert.equal(percentage.quantity('12.5'), '12.5');
});

test('density ticks enclose finite numerical maxima and keep numerical labels concise', () => {
  for (const value of [0.0000023, 0.22939, 1, 2.1, 17, 79, 100, 10001]) {
    const axis = densityAxis(value);
    assert.ok(axis.maximum >= value);
    assert.equal(axis.ticks[0], 0);
    assert.equal(axis.ticks[axis.ticks.length - 1], axis.maximum);
    assert.ok(axis.ticks.length >= 3 && axis.ticks.length <= 6);
    assert.ok(axis.ticks.every((tick, index) => index === 0 || tick > axis.ticks[index - 1]));
  }
  assert.equal(formatChartNumber(0.0625), '0.0625');
  assert.equal(formatChartNumber(1250), '1250');
  assert.equal(formatChartNumber(0), '0');
  assert.throws(() => densityAxis(Infinity), /positive finite/);
  assert.throws(() => densityAxis(0), /positive finite/);
});

test('narrow ranges at large offsets never show identical rounded tick numbers', () => {
  assert.deepEqual(continuousValueTicks('0', '20000', 1000).map(tick => tick.label), ['0', '5', '10', '15', '20']);

  // All five ticks would round to 1000000000; the caption still has exact bounds.
  assert.deepEqual(continuousValueTicks('1000000000', '1000000100', 1), [
    { position: 0, label: 'min' },
    { position: 1, label: 'max' },
  ]);
});
