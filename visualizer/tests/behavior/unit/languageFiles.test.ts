import assert from 'node:assert/strict';
import test from 'node:test';
import { loadLanguageSource, serializeLanguageSource } from '../../../src/scenario-language/editor.ts';
import { MAX_SOURCE_BYTES } from '../../../src/scenario-language/lexer.ts';

const file = (source: string) => ({ size: new TextEncoder().encode(source).byteLength, text: async () => source });

// Raw comments, Unicode and line endings survive import/export; loading never applies a scenario.
test('language source files preserve raw drafts and use the shared compiler for diagnostics', async () => {
  const source = '# przykład\r\nrun 1s\r\nbalancer lb {}\r\n';
  const loaded = await loadLanguageSource(file(source), new AbortController().signal);
  assert.equal(loaded.source, source);
  assert.equal(loaded.diagnostic, null);
  assert.equal(serializeLanguageSource(loaded.source), source);

  const invalid = '# keep unfinished work\nrun';
  const draft = await loadLanguageSource(file(invalid), new AbortController().signal);
  assert.equal(draft.source, invalid);
  assert.ok(draft.diagnostic);
  assert.equal(serializeLanguageSource(draft.source), invalid);
});

// Bound bytes both before and after reading; reject empty/unreadable files without replacing a draft.
test('language source files reject oversize, empty, unreadable and cancelled inputs', async () => {
  const signal = new AbortController().signal;
  await assert.rejects(loadLanguageSource({ size: MAX_SOURCE_BYTES + 1, text: async () => '' }, signal), /2 MiB/);
  await assert.rejects(loadLanguageSource({ size: 1, text: async () => 'é'.repeat(MAX_SOURCE_BYTES) }, signal), /2 MiB/);
  await assert.rejects(loadLanguageSource(file('   '), signal), /empty/);
  await assert.rejects(loadLanguageSource({ size: 1, text: async () => { throw new Error(); } }, signal), /Could not read/);
  const cancelled = new AbortController();
  cancelled.abort();
  await assert.rejects(loadLanguageSource(file('run 1s'), cancelled.signal), /abort/i);
  assert.throws(() => serializeLanguageSource(' '.repeat(MAX_SOURCE_BYTES + 1)), /2 MiB/);
  assert.throws(() => serializeLanguageSource(''), /empty/);
});
