import normal from './normal.ts';
import exponential from './exponential.ts';
import gamma from './gamma.ts';
import studentT from './student-t.ts';
import beta from './beta.ts';

// Additional families are enabled in declaration order.
export const additionalFamilies = [normal, exponential, gamma, studentT, beta] as const;
