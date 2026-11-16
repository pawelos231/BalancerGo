import type { ScenarioSourceExample } from '../../sourceExamples.ts';
import definitionSource from '../../examples/sources/empirical-profile-definitions.lb?raw';
import invalidSource from '../../examples/sources/empirical-invalid-parameters.lb?raw';
import missingSource from '../../examples/sources/empirical-missing-parameter.lb?raw';

// Canonical editable examples; applying remains an explicit user action.
export default [
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'empirical-profile-definitions',
    title: 'Empirical profile definitions',
    description: 'Compiles 2 named empirical definitions into exact JSON. Runtime settings remain scalar.',
    source: definitionSource,
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'empirical-invalid-parameters',
    title: 'Empirical invalid parameters',
    description: 'Observation values cannot mix time and byte dimensions.',
    source: invalidSource,
    expectedDiagnostic: 'Empirical quantity parameters must use compatible units.',
  },
  {
    kind: 'sourceExample', category: 'distributions',
    id: 'empirical-missing-parameter',
    title: 'Empirical missing parameter',
    description: 'Omits required observations from an empirical definition.',
    source: missingSource,
    expectedDiagnostic: 'Empirical requires observations.',
  },
] satisfies readonly ScenarioSourceExample[];
