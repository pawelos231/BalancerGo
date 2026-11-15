// Package profile defines exact stochastic profiles independently of scenario fields.
package profile

// Kind identifies a profile family. A family is accepted once its definition is installed.
type Kind string

const (
	KindConstant         Kind = "constant"
	KindUniform          Kind = "uniform"
	KindNormal           Kind = "normal"
	KindExponential      Kind = "exponential"
	KindGamma            Kind = "gamma"
	KindStudentT         Kind = "studentT"
	KindBeta             Kind = "beta"
	KindLognormal        Kind = "lognormal"
	KindPareto           Kind = "pareto"
	KindWeibull          Kind = "weibull"
	KindTriangular       Kind = "triangular"
	KindBernoulli        Kind = "bernoulli"
	KindPoisson          Kind = "poisson"
	KindDiscreteWeighted Kind = "discreteWeighted"
	KindEmpirical        Kind = "empirical"
)

// Unit is the canonical base unit of every quantity in a profile.
type Unit string

const (
	UnitNanoseconds   Unit = "nanoseconds"
	UnitBytes         Unit = "bytes"
	UnitBitsPerSecond Unit = "bitsPerSecond"
	UnitNumber        Unit = "number"
	UnitPercentage    Unit = "percentage"
)

// WeightedEntry preserves an exact outcome and its nonnegative relative weight.
type WeightedEntry struct {
	Value  string `json:"value"`
	Weight string `json:"weight"`
}

// Profile keeps exact decimal strings and distinguishes missing parameters from zero.
// Each family accepts only its own fields. Use DecodeProfile for strict JSON handling.
type Profile struct {
	Kind             Kind            `json:"kind"`
	Unit             Unit            `json:"unit"`
	Value            *string         `json:"value,omitempty"`
	Min              *string         `json:"min,omitempty"`
	Max              *string         `json:"max,omitempty"`
	Mean             *string         `json:"mean,omitempty"`
	Stddev           *string         `json:"stddev,omitempty"`
	Shape            *string         `json:"shape,omitempty"`
	Scale            *string         `json:"scale,omitempty"`
	Location         *string         `json:"location,omitempty"`
	DegreesOfFreedom *string         `json:"degreesOfFreedom,omitempty"`
	Alpha            *string         `json:"alpha,omitempty"`
	Beta             *string         `json:"beta,omitempty"`
	Median           *string         `json:"median,omitempty"`
	LogStddev        *string         `json:"logStddev,omitempty"`
	Mode             *string         `json:"mode,omitempty"`
	Probability      *string         `json:"probability,omitempty"`
	Entries          []WeightedEntry `json:"entries,omitempty"`
	Observations     []string        `json:"observations,omitempty"`
}

// scalarField connects a JSON name to its typed optional field.
type scalarField struct {
	name   string
	target **string
}

func scalarFields(definition *Profile) []scalarField {
	return []scalarField{
		{"value", &definition.Value},
		{"min", &definition.Min},
		{"max", &definition.Max},
		{"mean", &definition.Mean},
		{"stddev", &definition.Stddev},
		{"shape", &definition.Shape},
		{"scale", &definition.Scale},
		{"location", &definition.Location},
		{"degreesOfFreedom", &definition.DegreesOfFreedom},
		{"alpha", &definition.Alpha},
		{"beta", &definition.Beta},
		{"median", &definition.Median},
		{"logStddev", &definition.LogStddev},
		{"mode", &definition.Mode},
		{"probability", &definition.Probability},
	}
}
