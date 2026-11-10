import type { Scenario } from './types';

export const MAX_SCENARIO_BYTES = 2 * 1024 * 1024;

const utf8 = new TextEncoder();
const sizeError = 'The scenario file exceeds the 2 MiB limit.';

// Keep imported JSON unchanged so the backend can reject ambiguous documents.
export async function readScenarioFile(file: Pick<File, 'size' | 'text'>): Promise<string> {
  if (file.size > MAX_SCENARIO_BYTES) {
    throw new Error(sizeError);
  }

  let text: string;

  try {
    text = await file.text();
  } catch {
    throw new Error('Could not read the scenario file. Select it again and try importing it.');
  }

  if (utf8.encode(text).byteLength > MAX_SCENARIO_BYTES) {
    throw new Error(sizeError);
  }

  const document = text.startsWith('\uFEFF') ? text.slice(1) : text;

  if (document.trim() === '') {
    throw new Error('The scenario file is empty. Select a file containing a scenario.');
  }

  return document;
}

// Editor and export use the same formatting, keeping large documents compact.
// An oversized draft can still be displayed and reduced before Apply or export.
export function formatScenarioDocument(scenario: Scenario): string {
  const pretty = `${JSON.stringify(scenario, null, 2)}\n`;
  return utf8.encode(pretty).byteLength <= MAX_SCENARIO_BYTES
    ? pretty
    : JSON.stringify(scenario);
}

// Refuse oversized downloads while allowing the editor to display them for repair.
export function serializeScenario(scenario: Scenario): string {
  try {
    const document = formatScenarioDocument(scenario);
    if (utf8.encode(document).byteLength <= MAX_SCENARIO_BYTES) {
      return document;
    }
  } catch {
    throw new Error('Could not export the scenario as JSON. Validate it and try again.');
  }

  throw new Error('The scenario exceeds the 2 MiB export limit. Reduce its size before downloading it.');
}
