import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/lognormal-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/lognormal-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/lognormal-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'lognormal-profile-definitions',
    title: 'Lognormal profile definitions',
    description: 'Compiles 2 named lognormal definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'lognormal-invalid-parameters',
    title: 'Lognormal invalid parameters',
    description: 'A zero log standard deviation would collapse the continuous density.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile log-stddev must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'lognormal-missing-parameter',
    title: 'Lognormal missing parameter',
    description: 'Omits required log-stddev from a lognormal definition.',
    source: missingSource,
    expectedDiagnostic: 'Lognormal requires log-stddev.',
  },
] satisfies readonly ScenarioSourceExample[];
