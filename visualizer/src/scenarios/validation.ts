import type { Scenario } from './types.ts';
import { decodeScenario } from './document.ts';
import { parseExactJSON } from './json.ts';
import { checkScenario } from './timelineValidation.ts';

// Validate raw JSON before any browser rounding or duplicate-key replacement can occur.
export function parseScenario(document: string): Scenario {
  return validateScenario(parseExactJSON(document));
}

// Check the whole document and return an independent typed copy for Start.
export function validateScenario(input: unknown): Scenario {
  const scenario = decodeScenario(input);
  checkScenario(scenario);
  return scenario;
}
