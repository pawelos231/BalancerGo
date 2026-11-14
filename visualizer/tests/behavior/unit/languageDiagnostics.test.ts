import assert from 'node:assert/strict';
import test from 'node:test';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import type { DiagnosticRequest, DiagnosticResponse } from '../../../src/scenario-language/diagnostics.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

const base = 'run 1s\nbalancer lb {}\nbackend api-0 {}\n';

test('language diagnostics accept a complete source without returning a compiled scenario', () => {
  assert.equal(checkLanguageSource(base), null);
});

test('language diagnostics keep exact syntax, field and unit error spans', () => {
  const cases = [
    { tail: '@', text: '@', column: 1, message: /Unexpected character "@"/ },
    { tail: 'tcp { misspelled 1 }', text: 'misspelled', column: 7, message: /Unknown setting "misspelled"/ },
    { tail: 'tcp { mss 1GiB }', text: '1GiB', column: 11, message: /Unsupported unit "GiB"/ },
  ];

  for (const { tail, text, column, message } of cases) {
    const source = base + tail;
    const diagnostic = checkLanguageSource(source);
    assert.ok(diagnostic);
    assert.match(diagnostic.message, message);
    assert.deepEqual(diagnostic.range, {
      start: { offset: source.indexOf(text), line: 4, column },
      end: { offset: source.indexOf(text) + text.length, line: 4, column: column + text.length },
    });
    assert.deepEqual(Object.keys(diagnostic), ['message', 'range']);
    assert.ok(!(diagnostic instanceof LanguageError));
    assert.deepEqual(structuredClone(diagnostic), diagnostic);
  }
});

test('language diagnostics report only the first compiler error', () => {
  const source = `${base}tcp { first-typo 1 second-typo 2 }`;
  const diagnostic = checkLanguageSource(source);
  assert.ok(diagnostic);
  assert.match(diagnostic.message, /Unknown setting "first-typo"/);
  assert.equal(source.slice(diagnostic.range.start.offset, diagnostic.range.end.offset), 'first-typo');
});

test('language diagnostics preserve a zero-width EOF range for an unfinished block', () => {
  const source = `${base}tcp {`;
  const point = { offset: source.length, line: 4, column: 6 };
  const diagnostic = checkLanguageSource(source);
  assert.ok(diagnostic);
  assert.match(diagnostic.message, /Expected a closing brace/);
  assert.deepEqual(diagnostic.range, { start: point, end: point });
});

test('language diagnostics preserve CRLF lines and UTF-16 columns after Unicode text', () => {
  const source = '# \u{1f600}\r\nrun 1s\r\nbalancer lb {}\r\nbackend api-0 { name "\u{1f408}" typo 1 }';
  const diagnostic = checkLanguageSource(source);
  assert.ok(diagnostic);
  assert.deepEqual(diagnostic.range, {
    start: { offset: 56, line: 4, column: 27 },
    end: { offset: 60, line: 4, column: 31 },
  });
  assert.equal(source.slice(56, 60), 'typo');
});

test('language diagnostics let unexpected compiler failures escape', () => {
  assert.throws(() => checkLanguageSource(null as unknown as string), TypeError);
});

test('language diagnostic worker returns revisioned plain diagnostics and unexpected failures', async () => {
  const originalSelf = Object.getOwnPropertyDescriptor(globalThis, 'self');
  const responses: DiagnosticResponse[] = [];
  const worker = {
    onmessage: undefined as ((event: MessageEvent<DiagnosticRequest>) => void) | undefined,
    postMessage: (response: DiagnosticResponse) => responses.push(structuredClone(response)),
  };

  Object.defineProperty(globalThis, 'self', { value: worker, configurable: true });
  try {
    await import('../../../src/scenario-language/diagnostics.worker.ts');
    assert.ok(worker.onmessage);
    const send = (revision: number, source: string) =>
      worker.onmessage!({ data: { revision, source } } as MessageEvent<DiagnosticRequest>);

    send(8, base);
    send(9, `${base}@`);
    send(10, null as unknown as string);

    assert.deepEqual(responses[0], { revision: 8, diagnostic: null });
    assert.deepEqual(responses[1], { revision: 9, diagnostic: checkLanguageSource(`${base}@`) });
    assert.deepEqual(Object.keys(responses[2]), ['revision', 'diagnostic', 'failure']);
    assert.equal(responses[2].revision, 10);
    assert.equal(responses[2].diagnostic, null);
    assert.equal(typeof responses[2].failure, 'string');
    assert.ok(responses[2].failure);
  } finally {
    if (originalSelf) Object.defineProperty(globalThis, 'self', originalSelf);
    else Reflect.deleteProperty(globalThis, 'self');
  }
});
