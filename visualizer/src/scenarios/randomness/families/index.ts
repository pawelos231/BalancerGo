import normal from './normal.ts';
import exponential from './exponential.ts';
import gamma from './gamma.ts';
import studentT from './student-t.ts';
import beta from './beta.ts';
import lognormal from './lognormal.ts';
import pareto from './pareto.ts';
import weibull from './weibull.ts';
import triangular from './triangular.ts';

// Additional families are enabled in declaration order.
export const additionalFamilies = [normal, exponential, gamma, studentT, beta, lognormal, pareto, weibull, triangular] as const;
