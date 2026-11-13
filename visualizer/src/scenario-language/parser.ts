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
  private depth = 0;
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
    const named = ['balancer', 'backend', 'backends', 'clients', 'client', 'profile'].includes(keyword.text);
    if (!named && !['run', 'defaults', 'tcp', 'language'].includes(keyword.text)) {
      throw new LanguageError(`Unsupported declaration ${JSON.stringify(keyword.text)}.`, keyword.range);
    }

    let argument: Token | undefined;
    let profile: Token | undefined;
    if (keyword.text === 'run' || keyword.text === 'language') {
      argument = this.expect('number', `Expected a numeric value after ${keyword.text}.`);
    }
    if (named) {
      argument = this.expect('word', `Expected a name after ${keyword.text}.`);
      if (this.peek().kind === 'word' && this.peek().text === 'use') {
        this.take();
        profile = this.expect('word', 'Expected a profile name after use.');
      }
    }

    const properties = named || this.peek().kind === '{' ? this.block() : this.properties(false);
    return {
      keyword, argument, profile, properties,
      range: { start: keyword.range.start, end: this.tokens[this.index - 1].range.end },
    };
  }

  // Each field consumes one scalar, so "speed" alone points to its missing value.
  private properties(inBlock: boolean): Property[] {
    const properties: Property[] = [];
    while (this.peek().kind !== 'eof' && this.peek().kind !== '}') {
      if (this.peek().kind === 'newline') {
        if (!inBlock) break;
        this.take();
        continue;
      }
      const key = this.expect('word', 'Expected a setting name or end of declaration.');
      if (this.peek().kind === '{') {
        properties.push({ key, value: this.block() });
        continue;
      }
      const value = this.peek();
      if (!['word', 'number', 'string'].includes(value.kind)) {
        throw new LanguageError(`Expected a value for ${key.text}.`, value.range);
      }
      properties.push({ key, value: this.take() });
    }
    return properties;
  }

  // All nested settings share this reader; 64 levels bound recursion on pasted input.
  private block(): Property[] {
    const opening = this.expect('{', 'Expected an opening brace on the declaration line.');
    if (this.depth >= 64) {
      throw new LanguageError('Blocks exceed the maximum nesting depth of 64.', opening.range);
    }

    this.depth++;
    const properties = this.properties(true);
    this.expect('}', 'Expected a closing brace.');
    this.depth--;
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
