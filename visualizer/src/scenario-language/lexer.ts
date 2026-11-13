import { LanguageError } from './source.ts';
import type { SourcePoint, SourceRange } from './source.ts';

export interface Token {
  kind: 'word' | 'newline' | '{' | '}' | 'eof';
  text: string;
  range: SourceRange;
}

export const MAX_SOURCE_BYTES = 2 * 1024 * 1024;
const maxTokens = 200_000;

// Turn source into tokens with locations; meaning and field limits are checked later.
export function tokenize(source: string): Token[] {
  return new Lexer(source).read();
}

class Lexer {
  private offset = 0;
  private line = 1;
  private column = 1;
  private readonly source: string;

  constructor(source: string) {
    this.source = source;
    if (new TextEncoder().encode(source).byteLength > MAX_SOURCE_BYTES) {
      throw new LanguageError('Source exceeds the 2 MiB limit.', this.range(this.point()));
    }
  }

  read(): Token[] {
    const tokens: Token[] = [];

    // A UTF-8 file may begin with a decoded BOM; offsets still refer to the original text.
    if (this.source[0] === '\uFEFF') this.advance();

    while (this.offset < this.source.length) {
      const character = this.source[this.offset];
      if (character === ' ' || character === '\t') {
        this.advance();
        continue;
      }

      const start = this.point();
      let kind: Token['kind'];

      if (character === '\n' || character === '\r') {
        kind = 'newline';
        this.advance();
      } else if (character === '{' || character === '}') {
        kind = character;
        this.advance();
      } else if (/[A-Za-z_]/.test(character)) {
        kind = 'word';
        while (/[A-Za-z0-9_-]/.test(this.source[this.offset] ?? '')) this.advance();
      } else {
        this.advance();
        throw new LanguageError(`Unexpected character ${JSON.stringify(character)}.`, this.range(start));
      }

      tokens.push({ kind, text: this.source.slice(start.offset, this.offset), range: this.range(start) });
      if (tokens.length > maxTokens) {
        throw new LanguageError('Source contains too many tokens.', this.range(start));
      }
    }

    const end = this.point();
    tokens.push({ kind: 'eof', text: '', range: this.range(end) });
    return tokens;
  }

  // CRLF advances one line, just like LF; no real time is involved.
  private advance(): void {
    const character = this.source[this.offset++];
    if (character === '\r' && this.source[this.offset] === '\n') this.offset++;

    if (character === '\r' || character === '\n') {
      this.line++;
      this.column = 1;
    } else {
      this.column++;
    }
  }

  private point(): SourcePoint {
    return { offset: this.offset, line: this.line, column: this.column };
  }

  private range(start: SourcePoint): SourceRange {
    return { start, end: this.point() };
  }
}
