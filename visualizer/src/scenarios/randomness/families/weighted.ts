import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'discreteWeighted',
  source: 'weighted',
  title: 'Weighted discrete',
  compilationAvailable: true,
  parameters: [
    { name: 'entries', role: 'entries' },
  ],
} satisfies FamilySpec;
