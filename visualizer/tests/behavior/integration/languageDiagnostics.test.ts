import assert from 'node:assert/strict';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

const base = 'run 1s\nbalancer lb {}\nbackend api-0 {}\n';

// A native cross-field failure should highlight the supplied ECN flag, not the first line.
test('language diagnostics locate native TCP dependency and timer errors', () => {
  const source = `${base}tcp {\n congestion none\n ecn true\n}`;
  assert.throws(() => compileScenario(source), error => {
    assert.ok(error instanceof LanguageError);
    assert.equal(error.range.start.offset, source.indexOf('true'));
    assert.match(error.message, /requires congestion control.*Check the related TCP/);
    return true;
  });

  const timers = `${base}tcp {\n rto { minimum 2s initial 1s }\n}`;
  assert.throws(() => compileScenario(timers), error => {
    assert.ok(error instanceof LanguageError);
    assert.equal(error.range.start.offset, timers.indexOf('rto'));
    assert.match(error.message, /Reorder the RTO limits/);
    return true;
  });
});

// Native paths for references and expanded resources lead back to their original source spans.
test('language diagnostics locate missing profiles and expanded backend settings', () => {
  const profile = 'run 1s\nbalancer lb {}\nbackend api-0 use missing {}';
  assert.throws(() => compileScenario(profile), error => {
    assert.ok(error instanceof LanguageError);
    assert.equal(error.range.start.offset, profile.indexOf('missing'));
    assert.match(error.message, /Declare that profile/);
    return true;
  });

  const bulk = 'run 9223372036854775807ns speed 0\nbalancer lb {}\nbackends api { count 2 host { cores 2 } }';
  assert.throws(() => compileScenario(bulk), error => {
    assert.ok(error instanceof LanguageError);
    assert.equal(error.range.start.offset, bulk.indexOf('host'));
    assert.match(error.message, /too large for this duration/);
    return true;
  });
});
