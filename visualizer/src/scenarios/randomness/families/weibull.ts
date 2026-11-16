import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'weibull',
  source: 'weibull',
  title: 'Weibull',
  bounded: true,
  compilationAvailable: true,
  parameters: [
    { name: 'scale', role: 'positiveQuantity' },
    { name: 'shape', role: 'shape' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
