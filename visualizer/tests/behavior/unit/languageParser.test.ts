import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
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
    assert.notEqual(property.value.kind, 'distribution');
    assert.ok('text' in property.value);
    return [property.key.text, property.value.text];
  }), [
    ['seed', '2333'], ['speed', '1'],
  ]);
  assert.equal(run.range.start.line, 2);
  assert.equal(program.range.end.offset, source.length);
});

test('language parser retains inline uniform parameters and their source ranges', () => {
  const source = 'backend api-0 { host { packet-time uniform { min 5us max 15us } cores 2 } }';
  const host = parse(source).declarations[0].properties[0].value;
  assert.ok(Array.isArray(host));
  const distribution = host[0].value;
  assert.ok(!Array.isArray(distribution));
  assert.equal(distribution.kind, 'distribution');
  assert.ok('parameters' in distribution);
  assert.equal(distribution.family.text, 'uniform');
  assert.deepEqual(distribution.parameters.map(parameter => {
    assert.ok(!Array.isArray(parameter.value) && 'text' in parameter.value);
    return [parameter.key.text, parameter.value.text];
  }), [['min', '5us'], ['max', '15us']]);
  assert.equal(source.slice(distribution.range.start.offset, distribution.range.end.offset),
    'uniform { min 5us max 15us }');
  assert.equal(distribution.range.start.column, source.indexOf('uniform') + 1);
  assert.equal(distribution.parameters[0].key.range.start.column, source.indexOf('min') + 1);
  assert.equal(host[1].key.text, 'cores');
});

test('language parser accepts uniform expressions in timeline settings and additions', () => {
  const program = parse(`at 1s {
    set backend api-0 { host { packet-time uniform { min 1us max 2us } } }
    add backend api-1 { host { packet-time uniform { min 3us max 4us } } }
}`);
  const [set, add] = program.timeline[0].commands;
  assert.equal(set.kind, 'set');
  assert.equal(add.kind, 'add');
  assert.ok('properties' in set && 'declaration' in add);
  for (const properties of [set.properties, add.declaration.properties]) {
    const host = properties[0].value;
    assert.ok(Array.isArray(host));
    assert.ok(!Array.isArray(host[0].value));
    assert.equal(host[0].value.kind, 'distribution');
  }
});

test('language parser preserves scalar uniform and leaves parameter validation to later passes', () => {
  const program = parse('profile uniform {}\nbackend api-0 use uniform { admin uniform }\ndefaults travel uniform {}');
  assert.equal(program.declarations[1].profile?.text, 'uniform');
  const scalar = program.declarations[1].properties[0].value;
  assert.ok(!Array.isArray(scalar) && 'text' in scalar);
  assert.equal(scalar.text, 'uniform');
  const distribution = program.declarations[2].properties[0].value;
  assert.ok(!Array.isArray(distribution) && 'parameters' in distribution);
  assert.deepEqual(distribution.parameters, []);
});

test('language parser locates unsupported distribution families and incomplete definitions', () => {
  assert.throws(() => parse('defaults travel unknown-family { min 1ms max 2ms }'),
    /1:17: Unsupported distribution "unknown-family"/);
  assert.throws(() => parse('defaults travel uniform { min 1ms'), /Expected a closing brace/);
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

// The full timeline fixture becomes syntax, with every operation still in source order.
test('language parser reads at blocks and all supported command shapes', () => {
  const source = readFileSync(new URL('../../../../examples/language/timeline.lb', import.meta.url), 'utf8');
  const program = parse(source);
  assert.deepEqual(program.timeline.map(block => block.at.text), ['30s', '60s', '90s']);
  assert.deepEqual(program.timeline[0].commands.map(command => command.kind), ['set', 'down', 'add']);
  assert.equal(program.timeline[2].commands[0].kind, 'stop');

  const block = parse('at 1s { set backend api-0 { host { cores 2 } }\n drain backend api-1 }').timeline[0];
  assert.deepEqual(block.commands.map(command => command.kind), ['set', 'drain']);
});

// Grammar rejects misplaced declarations and malformed targets before semantic checks.
test('language parser rejects incomplete or misplaced timeline commands', () => {
  assert.throws(() => parse('at 1s {}\nbackend api-0 {}'), /Initial declarations must precede/);
  assert.throws(() => parse('at 1s { down client regular-0 }'), /Expected backend/);
  assert.throws(() => parse('at 1s { add balancer other {} }'), /Add supports/);
  assert.throws(() => parse('at 1s { stop arrivals }'), /client group name/);
  assert.throws(() => parse('at 1s { up backend api-0 down backend api-1 }'), /newline or block end/);
  assert.throws(() => parse('at 1s { mystery value }'), /Unsupported timeline command/);
  assert.throws(() => parse('at 1s {'), /closing brace for at/);
});
