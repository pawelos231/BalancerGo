import type { FamilySpec } from '../types.ts';

// Declaration-stage metadata; the language stage enables named compilation.
export default {
  kind: 'studentT',
  source: 'student-t',
  title: 'Student t',
  bounded: true,
  compilationAvailable: false,
  parameters: [
    { name: 'location', role: 'quantity' },
    { name: 'scale', role: 'positiveQuantity' },
    { name: 'degreesOfFreedom', role: 'shape', source: 'degrees-of-freedom' },
    { name: 'min', role: 'quantity' },
    { name: 'max', role: 'quantity' },
  ],
} satisfies FamilySpec;
