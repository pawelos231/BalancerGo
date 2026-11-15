import assert from 'node:assert/strict';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

const base = 'run 10s\nbalancer lb {}\nbackend api-0 {}\n';
const distribution = 'uniform { min 5us max 15us }';

// A well-formed declaration is recognized everywhere, but cannot silently change a run yet.
test('uniform cannot be discarded from defaults, unused profiles or timeline commands', () => {
  const sources = [
    `${base}defaults travel ${distribution}`,
    `${base}profile unused { travel ${distribution} }`,
    `${base}backend extra-0 { host { packet-time ${distribution} } }`,
    `${base}at 1s { set backend api-0 host { packet-time ${distribution} } }`,
    `${base}at 1s { add backend extra-0 { host { packet-time ${distribution} } } }`,
  ];

  for (const source of sources) {
    assert.throws(() => compileScenario(source), error => {
      assert.ok(error instanceof LanguageError);
      assert.match(error.message, /Uniform distribution syntax is recognized/);
      assert.equal(error.range.start.offset, source.indexOf('uniform'));
      return true;
    });
  }
});

// The editor underlines the offending maximum when 15us is smaller than 20us.
test('uniform parameter errors reach the editor with the original source position', () => {
  const source = `${base}defaults travel uniform { min 20us max 15us }`;
  const diagnostic = checkLanguageSource(source);

  assert.ok(diagnostic);
  assert.match(diagnostic.message, /min must be less than max/);
  assert.equal(diagnostic.range.start.offset, source.indexOf('15us'));
  assert.equal(diagnostic.range.end.offset, source.indexOf('15us') + 4);
});

// uniform is still a valid profile ID; adding the distribution keyword must not reserve names.
test('existing scalar values and a profile named uniform compile as before', () => {
  const source = `run 10s
balancer lb {}
profile uniform { travel 5us }
backend api-0 use uniform { host { packet-time 10us } }
`;
  const scenario = compileScenario(source);
  const backend = scenario.states['0'].resources.backends[0];

  assert.equal(backend.network.profile, 'uniform');
  assert.equal(backend.host?.packetProcessingTimeNanoseconds, 10_000);
  assert.equal(scenario.fixed.network.profiles[0].config.packetTravelTimeNanoseconds, 5_000);
});
