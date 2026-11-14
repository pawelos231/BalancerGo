import type { ScenarioApi } from '../scenarios/api.ts';
import type { ValidatedScenario } from '../scenarios/types.ts';
import { readScenarioFile } from '../scenarios/files.ts';
import { compileScenario } from './compile.ts';
import { MAX_SOURCE_BYTES } from './lexer.ts';
import { LanguageError } from './source.ts';

// A small editable example, independent of whichever JSON preset is currently applied.
export const initialLanguageSource = `run 180s {
    seed 1
    speed 1
}
defaults {
    link 1GB
    travel 1ms
    queue 1024
}
tcp {
    congestion reno
    mss 1460B
    SACK false
    ECN false
}

balancer lb {
    ingress 1GB
    egress 1GB
}

backends api {
    count 2
    link 250MB
}

clients regular {
    count 8
    payload 1MB
    every 10ms
    link 10MB
}
`;

// Compilation is local; only ready JSON reaches the existing authoritative validation endpoint.
export async function validateLanguageDocument(
  source: string,
  api: Pick<ScenarioApi, 'validate'>,
  signal: AbortSignal,
): Promise<ValidatedScenario> {
  signal.throwIfAborted();
  const scenario = compileScenario(source);
  signal.throwIfAborted();
  const checked = await api.validate(scenario, signal);
  signal.throwIfAborted();
  return checked;
}

// Load editable text and use the same compiler for diagnostics; applying still requires an explicit action.
export async function loadLanguageSource(
  file: Pick<File, 'size' | 'text'>,
  signal: AbortSignal,
): Promise<{ source: string; diagnostic: LanguageError | null }> {
  signal.throwIfAborted();
  const source = await readScenarioFile(file);
  signal.throwIfAborted();
  try {
    compileScenario(source);
    return { source, diagnostic: null };
  } catch (error) {
    if (!(error instanceof LanguageError)) throw error;
    return { source, diagnostic: error };
  }
}

// Source export saves work in progress too, without turning it into JSON or losing comments.
export function serializeLanguageSource(source: string): string {
  if (new TextEncoder().encode(source).byteLength > MAX_SOURCE_BYTES) {
    throw new Error('The source file exceeds the 2 MiB limit.');
  }
  if (source.trim() === '') throw new Error('The source file is empty.');
  return source;
}

