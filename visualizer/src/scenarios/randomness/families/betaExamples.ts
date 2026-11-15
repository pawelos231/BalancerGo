import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/beta-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/beta-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/beta-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'beta-profile-definitions',
    title: 'Beta profile definitions',
    description: 'Declares named beta profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Beta declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'beta-invalid-parameters',
    title: 'Beta invalid parameters',
    description: 'Both beta shape parameters must be positive.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile alpha must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'beta-missing-parameter',
    title: 'Beta missing parameter',
    description: 'Omits required beta from a beta definition.',
    source: missingSource,
    expectedDiagnostic: 'Beta requires beta.',
  },
] satisfies readonly ScenarioSourceExample[];
