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

// Native errors name JSON paths; point to the nearest supplied field or its containing block.
export function locateValidationError(
  error: unknown,
  locations: ReadonlyMap<string, SourceRange>,
  fallback: SourceRange,
): LanguageError {
  if (error instanceof LanguageError) return error;
  const message = error instanceof Error ? error.message : 'Invalid compiled scenario.';
  const path = message.split(':', 1)[0].replace(/^scenario\./, '');
  let range = fallback;
  let bestLength = -1;

  for (const [candidate, origin] of locations) {
    const matches = path === candidate || path.startsWith(`${candidate}.`) || path.startsWith(`${candidate}[`);
    if (matches && candidate.length > bestLength) {
      range = origin;
      bestLength = candidate.length;
    }
  }

  let hint = '';
  if (message.includes('unknown profile')) hint = 'Declare that profile or choose an existing name.';
  else if (message.includes('minimum <= initial <= maximum')) hint = 'Reorder the RTO limits.';
  else if (message.includes('requires')) hint = 'Check the related TCP flags and congestion mode.';
  else if (message.includes('paced duration')) hint = 'Increase speed or use speed 0 for unpaced execution.';
  else if (message.includes('total reserved clients')) hint = 'Reduce client counts across the scenario.';
  else if (/exceeds|overflows|too large/.test(message)) hint = 'Reduce this value or the related resource limits.';
  return new LanguageError(hint ? `${message} ${hint}` : message, range);
}
