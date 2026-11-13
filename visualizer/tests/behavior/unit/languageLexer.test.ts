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

// Units are recognized as part of the token, but are not interpreted or rounded yet.
test('language lexer preserves exact numbers, suffixes and percentages', () => {
  const values = ['1460B', '1GB', '1EB', '0.5ms', '1%', '-9007199254740993', '18446744073709551615'];
  const tokens = tokenize(values.join(' '));
  assert.deepEqual(tokens.slice(0, -1).map(token => token.kind), values.map(() => 'number'));
  assert.deepEqual(tokens.slice(0, -1).map(token => token.text), values);

  // A later semantic step rejects unsupported units such as GiB.
  assert.equal(tokenize('1GiB')[0].text, '1GiB');
});

// Malformed decimals must not be split into several apparently valid numbers.
test('language lexer rejects malformed numeric spelling with its source position', () => {
  for (const value of ['01', '1.2.3ms', '1e3', '1e-3', '1E-3ms', '1e+3', '1-2', '-', '1%%', '1.']) {
    assert.throws(() => tokenize(`run\n  ${value}`), error => {
      assert.ok(error instanceof LanguageError);
      assert.match(error.message, /2:3: Invalid number/);
      assert.equal(error.range.end.offset - error.range.start.offset, value.length);
      return true;
    });
  }
  assert.deepEqual(tokenize('-1 -2').slice(0, -1).map(token => token.text), ['-1', '-2']);
  assert.throws(() => tokenize('+1'), /Unexpected character/);
});

// A # inside a display name is text; a # after the quote begins a comment.
test('language lexer distinguishes comments from escaped quoted text', () => {
  const tokens = tokenize('# heading\r\nname "API #1 \\"fast\\" \\u0041" # ignored\nhost {}');
  const text = tokens.find(token => token.kind === 'string');
  assert.ok(text);
  assert.equal(text.value, 'API #1 "fast" A');
  assert.equal(text.range.start.line, 2);
  assert.equal(text.range.start.column, 6);
  assert.deepEqual(tokens.filter(token => token.kind === 'word').map(token => token.text), ['name', 'host']);
  assert.equal(tokens.filter(token => token.kind === 'newline').length, 2);
});

// Bad escapes and literal newlines must identify the beginning of the quoted value.
test('language lexer reports invalid and unfinished strings', () => {
  for (const value of ['"bad\\q"', '"bad\\u12ZZ"', '"open', '"line\nbreak"', '"slash\\\n']) {
    assert.throws(() => tokenize(`name\n  ${value}`), /2:3: (Invalid string|Unterminated string)/);
  }
  assert.equal(tokenize('""')[0].value, '');
  assert.equal(tokenize('"😀"')[0].value, '😀');
});
