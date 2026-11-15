import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/student-t-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/student-t-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/student-t-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'student-t-profile-definitions',
    title: 'Student t profile definitions',
    description: 'Compiles 2 named student-t definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'student-t-invalid-parameters',
    title: 'Student t invalid parameters',
    description: 'Degrees of freedom must be a positive dimensionless value.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile degrees-of-freedom must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'student-t-missing-parameter',
    title: 'Student t missing parameter',
    description: 'Omits required degrees-of-freedom from a student-t definition.',
    source: missingSource,
    expectedDiagnostic: 'Student t requires degrees-of-freedom.',
  },
] satisfies readonly ScenarioSourceExample[];
