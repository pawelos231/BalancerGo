package uniform

import (
	"fmt"
	"math"
	"math/big"

	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// UniformFloatSampler approximates a continuous uniform profile with float64 values.
// Prepare it before a run: some valid decimal intervals contain no float64 value.
type UniformFloatSampler struct {
	lowerBound float64
	upperBound float64
}

// NewUniformFloatSampler copies uniform bounds without drawing randomness.
// Constants use IntegerSampler for now; floating constant conversion is separate.
func NewUniformFloatSampler(definition profile.Profile) (*UniformFloatSampler, error) {
	if err := profile.ValidateProfile(definition); err != nil {
		return nil, err
	}
	if definition.Kind != profile.KindUniform {
		return nil, fmt.Errorf("randomness: floating uniform sampler requires a uniform profile")
	}

	minimum, _ := profile.ParseDecimal(*definition.Min)
	maximum, _ := profile.ParseDecimal(*definition.Max)
	lower := ceilFloat64(minimum)
	upper := ceilFloat64(maximum)
	if lower >= upper {
		return nil, fmt.Errorf("randomness: uniform interval contains no float64 value")
	}
	return &UniformFloatSampler{lowerBound: lower, upperBound: upper}, nil
}

// Sample uses the caller's stream and stays inside the original decimal interval.
// One representable result needs no draw; otherwise the approximation uses 53 random bits.
func (sampler *UniformFloatSampler) Sample(rng source.RNG) float64 {
	if math.Nextafter(sampler.lowerBound, math.Inf(1)) == sampler.upperBound {
		return sampler.lowerBound
	}

	// For [2,6), a fraction of 0.5 produces 4.
	fraction := float64(rng.Uint64()>>11) * (1.0 / (1 << 53))
	value := sampler.lowerBound + fraction*(sampler.upperBound-sampler.lowerBound)

	// Rounding must not turn an almost-last sample into the excluded upper bound.
	if value >= sampler.upperBound {
		return math.Nextafter(sampler.upperBound, sampler.lowerBound)
	}
	return value
}

// ceilFloat64 selects the first float64 at or above an exact decimal bound.
// Using it for both bounds preserves [min,max), including nonrepresentable decimals.
func ceilFloat64(value *big.Rat) float64 {
	rounded, _ := value.Float64()
	if new(big.Rat).SetFloat64(rounded).Cmp(value) < 0 {
		return math.Nextafter(rounded, math.Inf(1))
	}
	return rounded
}
