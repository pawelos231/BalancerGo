// Offsets count UTF-16 code units; line and column start at one for editor messages.
export interface SourcePoint {
  offset: number;
  line: number;
  column: number;
}

export interface SourceRange {
  start: SourcePoint;
  end: SourcePoint;
}

// Keep the original location, for example 3:5 for a bad field on the third line.
export class LanguageError extends Error {
  readonly range: SourceRange;

  constructor(message: string, range: SourceRange) {
    super(`${range.start.line}:${range.start.column}: ${message}`);
    this.name = 'LanguageError';
    this.range = range;
  }
}
