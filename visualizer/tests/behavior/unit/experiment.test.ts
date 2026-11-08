import assert from 'node:assert/strict';
import test from 'node:test';
import { isExperimentContext } from '../../../src/experiment.ts';
import { experimentFixture } from '../../scenarioFixture.ts';

test('experiment context accepts zero speed, false flags and optional build provenance', () => {
  const context = experimentFixture();
  assert.equal(isExperimentContext(context), true);
  delete context.build.vcsRevision;
  delete context.build.vcsModified;
  assert.equal(isExperimentContext(context), true);
  context.execution = { headless: true, speed: 0.5, traceEnabled: true };
  context.build.vcsRevision = '';
  context.build.vcsModified = true;
  assert.equal(isExperimentContext(context), true);
});

test('experiment context rejects missing or malformed identities', () => {
  const context = experimentFixture();
  for (const value of [undefined, null, [], {}, 'experiment']) {
    assert.equal(isExperimentContext(value), false);
  }

  const invalidIdentities = [
    { ...context, model: '' },
    { ...context, model: 1 },
    { ...context, scenarioSha256: null },
    { ...context, scenarioSha256: 'a'.repeat(63) },
    { ...context, scenarioSha256: 'a'.repeat(65) },
    { ...context, scenarioSha256: 'A'.repeat(64) },
    { ...context, scenarioSha256: 'g'.repeat(64) },
  ];
  for (const invalid of invalidIdentities) {
    assert.equal(isExperimentContext(invalid), false);
  }
});

test('experiment context rejects incomplete execution settings and invalid speed', () => {
  const context = experimentFixture();
  const invalidExecutions = [
    undefined,
    null,
    [],
    { ...context.execution, headless: 'false' },
    { ...context.execution, traceEnabled: undefined },
  ];
  for (const execution of invalidExecutions) {
    assert.equal(isExperimentContext({ ...context, execution }), false);
  }

  for (const speed of [-1, Infinity, -Infinity, NaN, '0']) {
    const execution = { ...context.execution, speed };
    assert.equal(isExperimentContext({ ...context, execution }), false);
  }
});

test('experiment context rejects missing build fields and malformed optional flags', () => {
  const context = experimentFixture();
  const invalidBuilds = [
    undefined,
    null,
    [],
    { ...context.build, goVersion: '' },
    { ...context.build, moduleVersion: undefined },
    { ...context.build, goos: '' },
    { ...context.build, goarch: '' },
    { ...context.build, vcsRevision: 1 },
    { ...context.build, vcsModified: 'false' },
  ];
  for (const build of invalidBuilds) {
    assert.equal(isExperimentContext({ ...context, build }), false);
  }
});
