import type { ScenarioSourceExample } from '../sourceExamples.ts';
import normal from './families/normalExamples.ts';
import exponential from './families/exponentialExamples.ts';
import gamma from './families/gammaExamples.ts';
import studentT from './families/student-tExamples.ts';
import beta from './families/betaExamples.ts';
import lognormal from './families/lognormalExamples.ts';
import pareto from './families/paretoExamples.ts';
import weibull from './families/weibullExamples.ts';
import triangular from './families/triangularExamples.ts';
import bernoulli from './families/bernoulliExamples.ts';

export const randomFamilyExamples: readonly ScenarioSourceExample[] = [
  ...normal,
  ...exponential,
  ...gamma,
  ...studentT,
  ...beta,
  ...lognormal,
  ...pareto,
  ...weibull,
  ...triangular,
  ...bernoulli,
];
