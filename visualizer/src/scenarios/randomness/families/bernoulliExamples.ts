import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/bernoulli-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/bernoulli-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/bernoulli-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'bernoulli-profile-definitions',
    title: 'Bernoulli profile definitions',
    description: 'Compiles 3 named bernoulli definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'bernoulli-invalid-parameters',
    title: 'Bernoulli invalid parameters',
    description: 'A probability cannot be greater than 1.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile probability must be between 0 and 1.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'bernoulli-missing-parameter',
    title: 'Bernoulli missing parameter',
    description: 'Omits required probability from a bernoulli definition.',
    source: missingSource,
    expectedDiagnostic: 'Bernoulli requires probability.',
  },
] satisfies readonly ScenarioSourceExample[];
