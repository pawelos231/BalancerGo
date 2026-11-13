import assert from 'node:assert/strict';
import test from 'node:test';
import { MAX_SOURCE_BYTES, tokenize } from '../../../src/scenario-language/lexer.ts';
import { LanguageError } from '../../../src/scenario-language/source.ts';

// The second line's field starts at column five, despite tabs and CRLF elsewhere.
test('language lexer retains names, braces, newlines and exact source positions', () => {
  const source = 'backend api-0 {\r\n    host { cores }\n}\t';
  const tokens = tokenize(source);
  assert.deepEqual(tokens.map(token => token.kind), [
    'word', 'word', '{', 'newline', 'word', '{', 'word', '}', 'newline', '}', 'eof',
  ]);
  assert.equal(tokens[1].text, 'api-0');
  assert.deepEqual(tokens[4].range.start, { offset: 21, line: 2, column: 5 });
  assert.equal(source.slice(tokens[4].range.start.offset, tokens[4].range.end.offset), 'host');
  assert.equal(tokens.at(-1)?.range.start.offset, source.length);
});

// A BOM is ignored as syntax, while an unexpected @ points to its original column.
test('language lexer accepts empty files and BOM and reports bad characters', () => {
  assert.deepEqual(tokenize('').map(token => token.kind), ['eof']);
  assert.equal(tokenize('\uFEFFbalancer lb {}')[0].range.start.offset, 1);
  assert.throws(() => tokenize('host\n  @'), error => {
    assert.ok(error instanceof LanguageError);
    assert.match(error.message, /2:3: Unexpected character "@"/);
    assert.equal(error.range.end.offset - error.range.start.offset, 1);
    return true;
  });
});

// Reject oversized input before creating a large token array.
test('language lexer bounds UTF-8 bytes and token count', () => {
  assert.throws(() => tokenize(' '.repeat(MAX_SOURCE_BYTES + 1)), /2 MiB/);
  assert.throws(() => tokenize('{} '.repeat(100_001)), /too many tokens/);
});
