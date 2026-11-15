import type { FamilySpec } from './types.ts';
import { additionalFamilies } from './families/index.ts';

// Modules enter this list when their declaration stage lands; future kinds stay rejected.
export const familySpecs: readonly FamilySpec[] = [
  {
    kind: 'constant', source: 'constant', title: 'Constant', compilationAvailable: false,
    parameters: [{ name: 'value', role: 'quantity' }],
  },
  {
    kind: 'uniform', source: 'uniform', title: 'Uniform', bounded: true, compilationAvailable: true,
    parameters: [{ name: 'min', role: 'quantity' }, { name: 'max', role: 'quantity' }],
  },
  ...additionalFamilies,
];

export function familyByKind(kind: unknown): FamilySpec | undefined {
  return familySpecs.find(family => family.kind === kind);
}

export function familyBySource(source: string): FamilySpec | undefined {
  return familySpecs.find(family => family.source === source || family.aliases?.includes(source));
}
