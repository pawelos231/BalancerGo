import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'poisson',
  source: 'poisson',
  title: 'Poisson',
  bounded: true,
  numberOnly: true,
  compilationAvailable: false,
  parameters: [
    { name: 'mean', role: 'nonnegativeShape' },
    { name: 'min', role: 'integerQuantity' },
    { name: 'max', role: 'integerQuantity' },
  ],
} satisfies FamilySpec;
