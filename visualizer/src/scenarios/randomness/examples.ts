import type { ScenarioSourceExample } from '../sourceExamples.ts';
import normal from './families/normalExamples.ts';
import exponential from './families/exponentialExamples.ts';
import gamma from './families/gammaExamples.ts';
import studentT from './families/student-tExamples.ts';
import beta from './families/betaExamples.ts';

export const randomFamilyExamples: readonly ScenarioSourceExample[] = [
  ...normal,
  ...exponential,
  ...gamma,
  ...studentT,
  ...beta,
];
