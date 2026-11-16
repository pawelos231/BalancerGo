import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/poisson-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/poisson-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/poisson-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'poisson-profile-definitions',
    title: 'Poisson profile definitions',
    description: 'Declares named poisson profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Poisson declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'poisson-invalid-parameters',
    title: 'Poisson invalid parameters',
    description: 'Poisson support bounds must be whole numbers.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile min must be an integer.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'poisson-missing-parameter',
    title: 'Poisson missing parameter',
    description: 'Omits required mean from a poisson definition.',
    source: missingSource,
    expectedDiagnostic: 'Poisson requires mean.',
  },
] satisfies readonly ScenarioSourceExample[];
