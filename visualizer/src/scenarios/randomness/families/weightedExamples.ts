import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/weighted-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/weighted-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/weighted-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weighted-profile-definitions',
    title: 'Weighted discrete profile definitions',
    description: 'Declares named weighted profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Weighted discrete declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weighted-invalid-parameters',
    title: 'Weighted discrete invalid parameters',
    description: 'All-zero weights cannot be normalized into probabilities.',
    source: invalidSource,
    expectedDiagnostic: 'Random weighted entries require a positive total weight.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weighted-missing-parameter',
    title: 'Weighted discrete missing parameter',
    description: 'Omits required entries from a weighted definition.',
    source: missingSource,
    expectedDiagnostic: 'Weighted discrete requires entries.',
  },
] satisfies readonly ScenarioSourceExample[];
