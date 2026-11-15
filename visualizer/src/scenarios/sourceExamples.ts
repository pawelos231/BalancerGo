import uniformCpuSource from './examples/sources/uniform-cpu-declaration.lb?raw';
import uniformTravelSource from './examples/sources/uniform-travel-declaration.lb?raw';
import uniformTimelineSource from './examples/sources/uniform-timeline-declaration.lb?raw';
import uniformInvalidRangeSource from './examples/sources/uniform-invalid-range.lb?raw';
import uniformInvalidUnitsSource from './examples/sources/uniform-invalid-units.lb?raw';
import uniformMissingBoundSource from './examples/sources/uniform-missing-bound.lb?raw';

// These editable sources demonstrate declarations and diagnostics before runtime integration.
export interface ScenarioSourceExample {
  readonly kind: 'sourceExample';
  readonly id: string;
  readonly category: 'distributions';
  readonly title: string;
  readonly description: string;
  readonly source: string;
  // The compiler adds source locations when checking the loaded draft.
  readonly expectedDiagnostic: string;
}

const declarationDiagnostic =
  'Uniform distribution syntax is recognized, but scenario compilation is not available yet.';

export const sourceExamples: readonly ScenarioSourceExample[] = [
  {
    kind: 'sourceExample',
    id: 'uniform-cpu-declaration',
    category: 'distributions',
    title: 'Uniform backend CPU declaration',
    description: 'Declares CPU time from 5us to 15us. Bounds are valid; compilation and sampling are pending.',
    source: uniformCpuSource,
    expectedDiagnostic: declarationDiagnostic,
  },
  {
    kind: 'sourceExample',
    id: 'uniform-travel-declaration',
    category: 'distributions',
    title: 'Uniform travel with mixed units',
    description: 'Declares travel from 500us to 1ms with compatible units. Compilation and sampling are pending.',
    source: uniformTravelSource,
    expectedDiagnostic: declarationDiagnostic,
  },
  {
    kind: 'sourceExample',
    id: 'uniform-timeline-declaration',
    category: 'distributions',
    title: 'Uniform timeline declaration',
    description: 'Starts with fixed CPU time and declares uniform bounds at 1s. Uniform compilation is pending.',
    source: uniformTimelineSource,
    expectedDiagnostic: declarationDiagnostic,
  },
  {
    kind: 'sourceExample',
    id: 'uniform-invalid-range',
    category: 'distributions',
    title: 'Uniform reversed range',
    description: 'Shows the validation error when min 15us is greater than max 5us.',
    source: uniformInvalidRangeSource,
    expectedDiagnostic: 'Uniform min must be less than max; use a scalar for a fixed value.',
  },
  {
    kind: 'sourceExample',
    id: 'uniform-invalid-units',
    category: 'distributions',
    title: 'Uniform incompatible units',
    description: 'Shows the validation error when a time minimum is paired with a byte maximum.',
    source: uniformInvalidUnitsSource,
    expectedDiagnostic: 'Uniform min and max must use compatible units.',
  },
  {
    kind: 'sourceExample',
    id: 'uniform-missing-bound',
    category: 'distributions',
    title: 'Uniform missing bound',
    description: 'Shows the validation error when max is omitted from a uniform declaration.',
    source: uniformMissingBoundSource,
    expectedDiagnostic: 'Uniform requires both min and max bounds.',
  },
];
