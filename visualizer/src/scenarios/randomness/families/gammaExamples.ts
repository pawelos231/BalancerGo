import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/gamma-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/gamma-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/gamma-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'gamma-profile-definitions',
    title: 'Gamma profile definitions',
    description: 'Declares named gamma profiles; scenario compilation is pending.',
    source: definitionSource,
    expectedDiagnostic: 'Gamma declaration is recognized; scenario compilation is not available yet.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'gamma-invalid-parameters',
    title: 'Gamma invalid parameters',
    description: 'The dimensionless gamma shape must be positive.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile shape must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'gamma-missing-parameter',
    title: 'Gamma missing parameter',
    description: 'Omits required shape from a gamma definition.',
    source: missingSource,
    expectedDiagnostic: 'Gamma requires shape.',
  },
] satisfies readonly ScenarioSourceExample[];
