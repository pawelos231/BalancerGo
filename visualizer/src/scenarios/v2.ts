import type { ScenarioV2 } from './v2Types.ts';
import { decodeScenarioV2 } from './v2Document.ts';
import { parseExactJSON } from './v2JSON.ts';
import { checkScenarioV2 } from './v2Validation.ts';

// Validate raw JSON before any browser rounding or duplicate-key replacement can occur.
export function parseScenarioV2(document: string): ScenarioV2 {
  return validateScenarioV2(parseExactJSON(document));
}

// Check the whole document and return an independent typed copy, ready for a future Start.
// Runtime admission and migration of the existing form remain later stages.
export function validateScenarioV2(input: unknown): ScenarioV2 {
  const scenario = decodeScenarioV2(input);
  checkScenarioV2(scenario);
  return scenario;
}
