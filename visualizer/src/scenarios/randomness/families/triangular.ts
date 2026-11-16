import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'triangular',
  source: 'triangular',
  title: 'Triangular',
  bounded: true,
  compilationAvailable: true,
  parameters: [
    { name: 'min', role: 'quantity' },
    { name: 'mode', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
