import assert from 'node:assert/strict';
import test from 'node:test';
import { scenarioDefaults } from '../../../src/scenario-language/defaults.ts';
import { parse } from '../../../src/scenario-language/parser.ts';
import { TimelineBuilder } from '../../../src/scenario-language/timeline.ts';
import type { SourceRange } from '../../../src/scenario-language/source.ts';
import { MAX_SCENARIO_BYTES } from '../../../src/scenarios/files.ts';

const block = (time: string) => parse(`at ${time} { set balancer egress 1MB }`).timeline[0];
const range = parse('').range;

// Mutating a later state leaves earlier CPU settings and arrays independent.
test('language timeline builder creates independent complete states at exact timestamps', () => {
  const initial = scenarioDefaults('9007199254740995');
  initial.fixed.execution.speed = 0;
  const builder = new TimelineBuilder(initial, new Map(), range);
  builder.append(block('9007199254740993ns'), state => {
    state.resources.loadBalancer.host!.cores = 2;
  });
  const result = builder.finish();
  assert.deepEqual(Object.keys(result.states), ['0', '9007199254740993']);
  assert.equal(result.states['0'].resources.loadBalancer.host?.cores, 1);
  assert.equal(result.states['9007199254740993'].resources.loadBalancer.host?.cores, 2);
  assert.deepEqual(result.states['9007199254740993'].workload.clientGroups, []);
});

// Equivalent units cannot create duplicate times, and neither zero nor the run end is a new state.
test('language timeline builder rejects unordered, duplicate and out-of-run timestamps', () => {
  const builder = new TimelineBuilder(scenarioDefaults(10_000_000_000), new Map(), range);
  builder.append(block('2s'), () => {});
  for (const time of ['1s', '2000ms', '10s', '11s', '0ns']) {
    assert.throws(() => builder.append(block(time), () => {}), /increase strictly|between/);
  }
  assert.throws(() => builder.append(parse('at 3s {}').timeline[0], () => {}), /at least one command/);
});

// A callback failure or excessive JSON expansion cannot append a half-built state.
test('language timeline builder keeps failed changes private and bounds expanded JSON', () => {
  const initial = scenarioDefaults(10_000_000_000);
  const origins = new Map();
  const builder = new TimelineBuilder(initial, origins, range);
  assert.throws(() => builder.append(block('1s'), (state, writer, path) => {
    state.resources.loadBalancer.algorithm = 'random';
    writer.locations.set(`${path}.resources.loadBalancer`, range);
    throw new Error('change rejected');
  }), /change rejected/);
  assert.deepEqual(Object.keys(initial.states), ['0']);
  assert.equal(origins.size, 0);
  assert.equal(initial.states['0'].resources.loadBalancer.algorithm, 'roundRobin');
  assert.throws(() => builder.append(block('1s'), state => {
    state.resources.backends.push({
      id: 'api-0', name: 'x'.repeat(MAX_SCENARIO_BYTES), address: '10.0.0.1', port: 80,
      network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    });
  }), /exceeds 2 MiB/);
  assert.deepEqual(Object.keys(initial.states), ['0']);
});

// Growing history must not make each new block rescan every earlier state's source locations.
test('language timeline source-origin work stays linear as history grows', () => {
  class ObservedOrigins extends Map<string, SourceRange> {
    visited = 0;
    *[Symbol.iterator]() {
      for (const entry of super[Symbol.iterator]()) {
        this.visited++;
        yield entry;
      }
    }
  }
  const origins = new ObservedOrigins();
  for (let index = 0; index < 30; index++) {
    origins.set(`states["0"].resources.field${index}`, range);
  }
  const builder = new TimelineBuilder(scenarioDefaults(1_000_000_000), origins, range);
  for (let index = 1; index <= 100; index++) builder.append(block(`${index}ns`), () => {});
  assert.ok(origins.visited <= 30 * 101, 'Historical origins must not be repeatedly scanned.');
  assert.equal(Object.keys(builder.finish().states).length, 101);
});
