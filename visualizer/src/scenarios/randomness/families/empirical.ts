import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'empirical',
  source: 'empirical',
  title: 'Empirical',
  compilationAvailable: true,
  parameters: [
    { name: 'observations', role: 'observations' },
  ],
} satisfies FamilySpec;
