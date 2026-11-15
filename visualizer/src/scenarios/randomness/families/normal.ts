import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'normal',
  source: 'normal',
  title: 'Normal',
  bounded: true,
  compilationAvailable: true,
  parameters: [
    { name: 'mean', role: 'quantity' },
    { name: 'stddev', role: 'positiveQuantity' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
