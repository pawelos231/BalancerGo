import { tokenize } from './lexer.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { Declaration, Program, Property } from './syntax.ts';

// Read syntax without applying defaults, converting units or constructing a scenario.
export function parse(source: string): Program {
  return new Parser(tokenize(source)).read();
}

class Parser {
  private index = 0;
  private readonly tokens: Token[];

  constructor(tokens: Token[]) {
    this.tokens = tokens;
  }

  read(): Program {
    const declarations: Declaration[] = [];
    while (this.peek().kind !== 'eof') {
      if (this.peek().kind === 'newline') {
        this.take();
        continue;
      }
      declarations.push(this.declaration());
    }

    return {
      declarations,
      range: { start: { offset: 0, line: 1, column: 1 }, end: this.peek().range.end },
    };
  }

  private declaration(): Declaration {
    const keyword = this.expect('word', 'Expected a declaration.');
    if (!['run', 'defaults', 'tcp', 'language'].includes(keyword.text)) {
      throw new LanguageError(`Unsupported declaration ${JSON.stringify(keyword.text)}.`, keyword.range);
    }

    let argument: Token | undefined;
    if (keyword.text === 'run' || keyword.text === 'language') {
      argument = this.expect('number', `Expected a numeric value after ${keyword.text}.`);
    }
    const properties = this.properties();
    return {
      keyword, argument, properties,
      range: { start: keyword.range.start, end: this.tokens[this.index - 1].range.end },
    };
  }

  // Each field consumes one scalar, so "speed" alone points to its missing value.
  private properties(): Property[] {
    const properties: Property[] = [];
    while (this.peek().kind !== 'newline' && this.peek().kind !== 'eof') {
      const key = this.expect('word', 'Expected a setting name or end of declaration.');
      const value = this.peek();
      if (!['word', 'number', 'string'].includes(value.kind)) {
        throw new LanguageError(`Expected a value for ${key.text}.`, value.range);
      }
      properties.push({ key, value: this.take() });
    }
    return properties;
  }

  private expect(kind: Token['kind'], message: string): Token {
    if (this.peek().kind !== kind) throw new LanguageError(message, this.peek().range);
    return this.take();
  }

  private peek(): Token {
    return this.tokens[this.index];
  }

  private take(): Token {
    return this.tokens[this.index++];
  }
}
