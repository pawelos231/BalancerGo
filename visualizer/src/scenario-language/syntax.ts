import type { Token } from './lexer.ts';
import type { SourceRange } from './source.ts';

// A setting still carries its source token; for example speed -> the token "1".
export interface Property {
  key: Token;
  value: Token;
}

// Syntax only: run's argument is its duration; settings are checked in later passes.
export interface Declaration {
  keyword: Token;
  argument?: Token;
  properties: Property[];
  range: SourceRange;
}

export interface Program {
  declarations: Declaration[];
  range: SourceRange;
}
