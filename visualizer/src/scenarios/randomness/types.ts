export type RandomUnit = 'nanoseconds' | 'bytes' | 'bitsPerSecond' | 'number' | 'percentage';
interface QuantityProfile { readonly unit: RandomUnit }
interface BoundedProfile extends QuantityProfile { readonly min: string; readonly max: string }
export interface WeightedEntry { readonly value: string; readonly weight: string }

// Required parameters are specific to each family; decimal values stay exact strings.
export type RandomProfile =
  | (QuantityProfile & { readonly kind: 'constant'; readonly value: string })
  | (BoundedProfile & { readonly kind: 'uniform' })
  | (BoundedProfile & { readonly kind: 'normal'; readonly mean: string; readonly stddev: string })
  | (BoundedProfile & { readonly kind: 'exponential'; readonly mean: string })
  | (BoundedProfile & { readonly kind: 'gamma'; readonly shape: string; readonly scale: string })
  | (BoundedProfile & {
    readonly kind: 'studentT'; readonly location: string; readonly scale: string;
    readonly degreesOfFreedom: string;
  })
  | (BoundedProfile & { readonly kind: 'beta'; readonly alpha: string; readonly beta: string })
  | (BoundedProfile & { readonly kind: 'lognormal'; readonly median: string; readonly logStddev: string })
  | (BoundedProfile & { readonly kind: 'pareto'; readonly shape: string; readonly scale: string })
  | (BoundedProfile & { readonly kind: 'weibull'; readonly shape: string; readonly scale: string })
  | (BoundedProfile & { readonly kind: 'triangular'; readonly mode: string })
  | { readonly kind: 'bernoulli'; readonly unit: 'number'; readonly probability: string }
  | (BoundedProfile & {
    readonly kind: 'poisson'; readonly unit: 'number'; readonly mean: string;
  })
  | (QuantityProfile & { readonly kind: 'discreteWeighted'; readonly entries: readonly WeightedEntry[] })
  | (QuantityProfile & { readonly kind: 'empirical'; readonly observations: readonly string[] });

export type RandomKind = RandomProfile['kind'];
export type ParameterRole = 'quantity' | 'positiveQuantity' | 'shape' | 'nonnegativeShape'
  | 'probability' | 'integerQuantity' | 'entries' | 'observations';

export interface ParameterSpec {
  readonly name: string;
  readonly source?: string;
  readonly role: ParameterRole;
}

// Quantity parameters share unit; shapes, probabilities and weights are dimensionless.
export interface FamilySpec {
  readonly kind: RandomKind;
  readonly source: string;
  readonly aliases?: readonly string[];
  readonly title: string;
  readonly parameters: readonly ParameterSpec[];
  readonly bounded?: boolean;
  readonly numberOnly?: boolean;
  readonly compilationAvailable: boolean;
}
