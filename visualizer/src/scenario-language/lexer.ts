import { LanguageError } from './source.ts';
import type { SourcePoint, SourceRange } from './source.ts';

export interface Token {
  kind: 'word' | 'number' | 'string' | 'newline' | '{' | '}' | 'eof';
  text: string;
  value?: string;
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
      if (character === '#') {
        while (this.offset < this.source.length && !/[\r\n]/.test(this.source[this.offset])) {
          this.advance();
        }
        continue;
      }

      const start = this.point();
      let kind: Token['kind'];
      let value: string | undefined;

      if (character === '\n' || character === '\r') {
        kind = 'newline';
        this.advance();
      } else if (character === '{' || character === '}') {
        kind = character;
        this.advance();
      } else if (/[A-Za-z_]/.test(character)) {
        kind = 'word';
        while (/[A-Za-z0-9_-]/.test(this.source[this.offset] ?? '')) this.advance();
      } else if (/[0-9-]/.test(character)) {
        kind = 'number';
        this.readNumber(start);
      } else if (character === '"') {
        kind = 'string';
        value = this.readString(start);
      } else {
        this.advance();
        throw new LanguageError(`Unexpected character ${JSON.stringify(character)}.`, this.range(start));
      }

      tokens.push({ kind, text: this.source.slice(start.offset, this.offset), value, range: this.range(start) });
      if (tokens.length > maxTokens) {
        throw new LanguageError('Source contains too many tokens.', this.range(start));
      }
    }

    const end = this.point();
    tokens.push({ kind: 'eof', text: '', range: this.range(end) });
    return tokens;
  }

  // Keep 1.5GB or a large seed as text; field conversion happens after parsing.
  private readNumber(start: SourcePoint): void {
    if (this.source[this.offset] === '-') this.advance();
    while (/[0-9.A-Za-z%+-]/.test(this.source[this.offset] ?? '')) this.advance();

    const text = this.source.slice(start.offset, this.offset);
    const valid = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([A-Za-z]+|%)?$/.test(text);
    if (!valid) {
      throw new LanguageError('Invalid number; use a decimal value and optional unit.', this.range(start));
    }
  }

  // Skip escaped quotes, then let JSON check escapes such as \n and \u0041.
  private readString(start: SourcePoint): string {
    this.advance();
    while (this.offset < this.source.length) {
      const character = this.source[this.offset];
      if (character === '\r' || character === '\n') break;

      this.advance();
      if (character === '"') {
        const raw = this.source.slice(start.offset, this.offset);
        try {
          return JSON.parse(raw) as string;
        } catch {
          throw new LanguageError('Invalid string escape or control character.', this.range(start));
        }
      }
      if (character === '\\') {
        const escaped = this.source[this.offset];
        if (escaped === undefined || escaped === '\r' || escaped === '\n') break;
        this.advance();
      }
    }

    throw new LanguageError('Unterminated string; close it on the same line.', this.range(start));
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
