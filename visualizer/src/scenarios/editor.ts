import type { ScenarioApi } from './api.ts';
import { MAX_SCENARIO_BYTES } from './files.ts';
import type { ValidatedScenario } from './types.ts';
import { parseScenario } from './validation.ts';

// Check locally without replacing raw tokens before authoritative server validation.
export async function validateEditorDocument(
  document: string,
  api: Pick<ScenarioApi, 'validateDocument'>,
  signal: AbortSignal,
): Promise<ValidatedScenario> {
  signal.throwIfAborted();
  if (new TextEncoder().encode(document).byteLength > MAX_SCENARIO_BYTES) {
    throw new Error('The scenario JSON exceeds the 2 MiB limit.');
  }
  parseScenario(document);
  const result = await api.validateDocument(document, signal);
  signal.throwIfAborted();
  return result;
}
