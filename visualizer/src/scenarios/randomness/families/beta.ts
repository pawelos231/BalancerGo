import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'beta',
  source: 'beta',
  title: 'Beta',
  bounded: true,
  compilationAvailable: false,
  parameters: [
    { name: 'alpha', role: 'shape' },
    { name: 'beta', role: 'shape' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
