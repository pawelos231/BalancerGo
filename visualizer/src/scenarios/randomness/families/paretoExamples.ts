import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/pareto-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/pareto-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/pareto-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'pareto-profile-definitions',
    title: 'Pareto profile definitions',
    description: 'Declares named pareto profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Pareto declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'pareto-invalid-parameters',
    title: 'Pareto invalid parameters',
    description: 'A Pareto minimum cannot lie below the scale threshold.',
    source: invalidSource,
    expectedDiagnostic: 'Random pareto min must be at least scale.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'pareto-missing-parameter',
    title: 'Pareto missing parameter',
    description: 'Omits required scale from a pareto definition.',
    source: missingSource,
    expectedDiagnostic: 'Pareto requires scale.',
  },
] satisfies readonly ScenarioSourceExample[];
