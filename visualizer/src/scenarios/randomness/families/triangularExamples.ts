import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/triangular-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/triangular-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/triangular-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'triangular-profile-definitions',
    title: 'Triangular profile definitions',
    description: 'Declares named triangular profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Triangular declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'triangular-invalid-parameters',
    title: 'Triangular invalid parameters',
    description: 'The triangular mode cannot lie outside its support.',
    source: invalidSource,
    expectedDiagnostic: 'Random triangular mode must be between min and max.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'triangular-missing-parameter',
    title: 'Triangular missing parameter',
    description: 'Omits required mode from a triangular definition.',
    source: missingSource,
    expectedDiagnostic: 'Triangular requires mode.',
  },
] satisfies readonly ScenarioSourceExample[];
