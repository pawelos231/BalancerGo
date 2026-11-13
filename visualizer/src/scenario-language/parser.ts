import { tokenize } from './lexer.ts';
import type { Token } from './lexer.ts';
import { LanguageError } from './source.ts';
import type { Command, Declaration, Program, Property, TimelineBlock } from './syntax.ts';

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
    const timeline: TimelineBlock[] = [];
    while (this.peek().kind !== 'eof') {
      if (this.peek().kind === 'newline') {
        this.take();
        continue;
      }
      if (this.peek().kind === 'word' && this.peek().text === 'at') {
        timeline.push(this.timelineBlock());
      } else {
        if (timeline.length > 0) {
          throw new LanguageError('Initial declarations must precede all at blocks.', this.peek().range);
        }
        declarations.push(this.declaration());
      }
    }

    return {
      declarations, timeline,
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

  // Preserve command order and raw timestamps; chronology is checked during compilation.
  private timelineBlock(): TimelineBlock {
    const opening = this.take();
    const at = this.expect('number', 'Expected a timestamp after at.');
    this.expect('{', 'Expected an opening brace on the declaration line.');
    const commands: Command[] = [];

    while (this.peek().kind !== '}' && this.peek().kind !== 'eof') {
      if (this.peek().kind === 'newline') {
        this.take();
        continue;
      }
      commands.push(this.command());
      if (!['newline', '}', 'eof'].includes(this.peek().kind)) {
        throw new LanguageError('Expected a newline or block end after the command.', this.peek().range);
      }
    }
    const closing = this.expect('}', 'Expected a closing brace for at.');
    return { at, commands, range: { start: opening.range.start, end: closing.range.end } };
  }

  private command(): Command {
    const keyword = this.expect('word', 'Expected a timeline command.');
    const range = () => ({ start: keyword.range.start, end: this.tokens[this.index - 1].range.end });

    if (keyword.text === 'add') {
      if (!['backend', 'backends', 'clients'].includes(this.peek().text)) {
        throw new LanguageError('Add supports backend, backends or clients.', this.peek().range);
      }
      const declaration = this.declaration();
      return { kind: 'add', declaration, range: range() };
    }
    if (keyword.text === 'set') {
      const target = this.expect('word', 'Expected a resource type after set.');
      if (!['balancer', 'backend', 'clients', 'client'].includes(target.text)) {
        throw new LanguageError('Set supports balancer, backend, clients or client.', target.range);
      }
      const name = target.text === 'balancer' ? undefined : this.expect('word', 'Expected a resource name.');
      const properties = this.peek().kind === '{' ? this.block() : this.properties(false);
      return { kind: 'set', target, name, properties, range: range() };
    }
    if (keyword.text === 'down' || keyword.text === 'up' || keyword.text === 'drain') {
      this.expectWord('backend');
      const name = this.expect('word', 'Expected a backend name.');
      return { kind: keyword.text, name, range: range() };
    }
    if (keyword.text === 'stop') {
      this.expectWord('arrivals');
      const name = this.expect('word', 'Expected a client group name.');
      return { kind: 'stop', name, range: range() };
    }
    throw new LanguageError(`Unsupported timeline command ${JSON.stringify(keyword.text)}.`, keyword.range);
  }

  private expectWord(text: string): void {
    const token = this.expect('word', `Expected ${text}.`);
    if (token.text !== text) throw new LanguageError(`Expected ${text}.`, token.range);
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
