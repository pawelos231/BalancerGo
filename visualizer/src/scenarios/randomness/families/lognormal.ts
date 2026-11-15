import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'lognormal',
  source: 'lognormal',
  title: 'Lognormal',
  bounded: true,
  compilationAvailable: false,
  parameters: [
    { name: 'median', role: 'positiveQuantity' },
    { name: 'logStddev', role: 'shape', source: 'log-stddev' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
