import type { ScenarioSourceExample } from '../sourceExamples.ts';
import normal from './families/normalExamples.ts';
import exponential from './families/exponentialExamples.ts';

export const randomFamilyExamples: readonly ScenarioSourceExample[] = [
  ...normal,
  ...exponential,
];
