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
  assert.deepEqual(run.properties.map(property => {
    assert.ok(!Array.isArray(property.value));
    return [property.key.text, property.value.text];
  }), [
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
  assert.throws(() => parse('tcp {'), /Expected a closing brace/);
  assert.deepEqual(parse('# no declarations\n').declarations, []);
});

// The same block reader handles host, directions and TCP settings at any nesting level.
test('language parser reads resources, profile selection and nested setting blocks', () => {
  const program = parse(`
profile mobile { link 10MB }
clients phones use mobile {
    count 20
    to-lb { travel 0ms }
}
backend api-0 { host { cores 4 packet-time 5us } }
tcp { rto { mode adaptive } }
`);
  const clients = program.declarations[1];
  assert.equal(clients.argument?.text, 'phones');
  assert.equal(clients.profile?.text, 'mobile');
  assert.ok(Array.isArray(clients.properties[1].value));
  assert.equal(clients.properties[1].value[0].key.text, 'travel');
  assert.equal(program.declarations[2].range.end.line, 7);
});

// A missing brace and excessive recursion are located errors, never a native stack overflow.
test('language parser bounds nested blocks and rejects incomplete resource headers', () => {
  assert.throws(() => parse('backend api-0\n{}'), /opening brace on the declaration line/);
  assert.throws(() => parse('clients group use {}'), /profile name after use/);
  assert.throws(() => parse('backend api-0 { host { cores 4 }'), /closing brace/);
  assert.throws(() => parse(`tcp { ${'rto { '.repeat(64)}mode adaptive${' }'.repeat(65)}`), /nesting depth/);
});
