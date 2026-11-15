import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'gamma',
  source: 'gamma',
  title: 'Gamma',
  bounded: true,
  compilationAvailable: false,
  parameters: [
    { name: 'shape', role: 'shape' },
    { name: 'scale', role: 'positiveQuantity' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
