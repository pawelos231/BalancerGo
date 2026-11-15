import type { Token } from './lexer.ts';
import type { SourceRange } from './source.ts';

// A setting still carries its source token; for example speed -> the token "1".
export interface Property {
  key: Token;
  value: Token | Property[] | DistributionExpression;
}

// Keep distribution parameters as source syntax; later passes check units and domains.
export interface DistributionExpression {
  kind: 'distribution';
  family: Token;
  parameters: Property[];
  range: SourceRange;
}

// Syntax only: run's argument is its duration; settings are checked in later passes.
export interface Declaration {
  keyword: Token;
  argument?: Token;
  profile?: Token;
  properties: Property[];
  range: SourceRange;
}

export interface Program {
  declarations: Declaration[];
  timeline: TimelineBlock[];
  range: SourceRange;
}

// Commands describe intended changes; only compilation will turn them into full states.
export type Command =
  | { kind: 'set'; target: Token; name?: Token; properties: Property[]; range: SourceRange }
  | { kind: 'add'; declaration: Declaration; range: SourceRange }
  | { kind: 'down' | 'up' | 'drain'; name: Token; range: SourceRange }
  | { kind: 'stop' | 'remove'; name: Token; range: SourceRange };

export interface TimelineBlock {
  at: Token;
  commands: Command[];
  range: SourceRange;
}
