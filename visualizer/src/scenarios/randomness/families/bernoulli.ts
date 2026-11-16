import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'bernoulli',
  source: 'bernoulli',
  title: 'Bernoulli',
  numberOnly: true,
  compilationAvailable: false,
  parameters: [
    { name: 'probability', role: 'probability' },
  ],
} satisfies FamilySpec;
