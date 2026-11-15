import normal from './normal.ts';
import exponential from './exponential.ts';

// Additional families are enabled in declaration order.
export const additionalFamilies = [normal, exponential] as const;
