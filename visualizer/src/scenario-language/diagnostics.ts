import { compileScenario } from './compile.ts';
import { LanguageError } from './source.ts';
import type { SourceRange } from './source.ts';

// A located language error that can cross the worker boundary as plain data.
export interface SourceDiagnostic {
  message: string;
  range: SourceRange;
}

// A draft and the revision that identifies its result.
export interface DiagnosticRequest {
  revision: number;
  source: string;
}

// The matching revision carries a language error, an unexpected failure, or neither.
export interface DiagnosticResponse {
  revision: number;
  diagnostic: SourceDiagnostic | null;
  failure?: string;
}

// Use the same checks as Apply, returning only the first located language error.
export function checkLanguageSource(source: string): SourceDiagnostic | null {
  try {
    compileScenario(source);
    return null;
  } catch (error) {
    if (!(error instanceof LanguageError)) throw error;

    // Send plain data across the worker boundary, preserving UTF-16 source offsets.
    return {
      message: error.message,
      range: {
        start: { ...error.range.start },
        end: { ...error.range.end },
      },
    };
  }
}
