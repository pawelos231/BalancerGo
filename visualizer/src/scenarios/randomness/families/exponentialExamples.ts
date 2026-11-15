import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/exponential-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/exponential-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/exponential-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'exponential-profile-definitions',
    title: 'Exponential profile definitions',
    description: 'Declares named exponential profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Exponential declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'exponential-invalid-parameters',
    title: 'Exponential invalid parameters',
    description: 'The exponential mean must be positive.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile mean must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'exponential-missing-parameter',
    title: 'Exponential missing parameter',
    description: 'Omits required mean from an exponential definition.',
    source: missingSource,
    expectedDiagnostic: 'Exponential requires mean.',
  },
] satisfies readonly ScenarioSourceExample[];
