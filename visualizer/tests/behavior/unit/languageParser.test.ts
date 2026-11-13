import assert from 'node:assert/strict';
import test from 'node:test';
import { parse } from '../../../src/scenario-language/parser.ts';

// Parsing retains literals: unit conversion and field validation belong to later passes.
test('language parser reads scalar declarations with source locations', () => {
  const source = 'language 1\nrun 180s seed 2333 speed 1\ndefaults link 1GB\ntcp congestion reno mss 1460B';
  const program = parse(source);
  assert.deepEqual(program.declarations.map(item => item.keyword.text), ['language', 'run', 'defaults', 'tcp']);
  const run = program.declarations[1];
  assert.equal(run.argument?.text, '180s');
  assert.deepEqual(run.properties.map(property => [property.key.text, property.value.text]), [
    ['seed', '2333'], ['speed', '1'],
  ]);
  assert.equal(run.range.start.line, 2);
  assert.equal(program.range.end.offset, source.length);
});

// A missing value and a stray token must fail at the place that cannot be parsed.
test('language parser rejects missing values and extra syntax', () => {
  assert.throws(() => parse('run 1s speed\n'), /1:13: Expected a value for speed/);
  assert.throws(() => parse('run\n'), /Expected a numeric value after run/);
  assert.throws(() => parse('run 1s 3'), /Expected a setting name/);
  assert.throws(() => parse('unknown value'), /Unsupported declaration/);
  assert.throws(() => parse('tcp {}'), /Expected a setting name/);
  assert.deepEqual(parse('# no declarations\n').declarations, []);
});
