import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/weibull-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/weibull-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/weibull-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weibull-profile-definitions',
    title: 'Weibull profile definitions',
    description: 'Compiles 2 named weibull definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weibull-invalid-parameters',
    title: 'Weibull invalid parameters',
    description: 'The Weibull shape must be positive.',
    source: invalidSource,
    expectedDiagnostic: 'Random profile shape must be positive.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'weibull-missing-parameter',
    title: 'Weibull missing parameter',
    description: 'Omits required shape from a weibull definition.',
    source: missingSource,
    expectedDiagnostic: 'Weibull requires shape.',
  },
] satisfies readonly ScenarioSourceExample[];
