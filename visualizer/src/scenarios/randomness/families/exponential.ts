import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'exponential',
  source: 'exponential',
  title: 'Exponential',
  bounded: true,
  compilationAvailable: true,
  parameters: [
    { name: 'mean', role: 'positiveQuantity' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
