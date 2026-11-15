import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/normal-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/normal-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/normal-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'normal-profile-definitions',
    title: 'Normal profile definitions',
    description: 'Compiles 2 named normal definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'normal-invalid-parameters',
    title: 'Normal invalid parameters',
    description: 'A zero standard deviation cannot define a normal density.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile stddev must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'normal-missing-parameter',
    title: 'Normal missing parameter',
    description: 'Omits required stddev from a normal definition.',
    source: missingSource,
    expectedDiagnostic: 'Normal requires stddev.',
  },
] satisfies readonly ScenarioSourceExample[];
