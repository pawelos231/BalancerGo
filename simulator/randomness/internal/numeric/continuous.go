// Package numeric contains numerical preparation shared by distribution families.
// Exact definition validation belongs to profile; these helpers can reject an
// exact definition whose requested floating point approximation is unavailable.
package numeric

import (
	"fmt"
	"math"
	"math/big"

	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

const probabilityTolerance = 1e-7

// Bounds holds the first allowed float and the first excluded float.
type Bounds struct{ Lower, Upper float64 }

// Check validates the complete definition and requires the requested family.
func Check(definition profile.Profile, kind string) error {
	if err := profile.ValidateProfile(definition); err != nil {
		return err
	}
	if string(definition.Kind) != kind {
		return fmt.Errorf("randomness: %s sampler requires a %s profile", kind, kind)
	}
	return nil
}

// Field converts one already validated parameter for numerical preparation.
func Field(text *string, name string, positive bool) (float64, error) {
	if text == nil {
		return 0, fmt.Errorf("randomness: missing %s", name)
	}
	exact, err := profile.ParseDecimal(*text)
	if err != nil {
		return 0, err
	}
	value, _ := exact.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) || (positive && value <= 0) {
		return 0, fmt.Errorf("randomness: %s is unavailable as a finite positive float64", name)
	}
	return value, nil
}

// ReadBounds rejects an exact interval with no finite representable result.
func ReadBounds(definition profile.Profile) (Bounds, error) {
	if definition.Min == nil || definition.Max == nil {
		return Bounds{}, fmt.Errorf("randomness: explicit min and max are required")
	}
	lo, err := profile.ParseDecimal(*definition.Min)
	if err != nil {
		return Bounds{}, err
	}
	hi, err := profile.ParseDecimal(*definition.Max)
	if err != nil {
		return Bounds{}, err
	}
	bounds := Bounds{ceil(lo), ceil(hi)}
	if bounds.Lower >= bounds.Upper || !finite(bounds.Lower) || !finite(bounds.Upper) {
		return Bounds{}, fmt.Errorf("randomness: interval contains no finite float64 value")
	}
	return bounds, nil
}

func ceil(value *big.Rat) float64 {
	rounded, _ := value.Float64()
	if new(big.Rat).SetFloat64(rounded).Cmp(value) < 0 {
		return math.Nextafter(rounded, math.Inf(1))
	}
	return rounded
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Fraction chooses a uniform midpoint of 2^52 equal cells, excluding 0 and 1.
func Fraction(rng source.RNG) float64 {
	return (float64(rng.Uint64()>>12) + 0.5) * (1.0 / (1 << 52))
}

// Singleton reports whether the floating approximation has one possible value.
func (bounds Bounds) Singleton() bool {
	return math.Nextafter(bounds.Lower, math.Inf(1)) == bounds.Upper
}

// Round corrects endpoint overshoot introduced by floating arithmetic.
func (bounds Bounds) Round(value float64) float64 {
	// These are representational endpoint corrections, not distribution clipping.
	if value < bounds.Lower {
		return bounds.Lower
	}
	if value >= bounds.Upper {
		return math.Nextafter(bounds.Upper, bounds.Lower)
	}
	return value
}

type quantileSampler struct {
	bounds  Bounds
	inverse func(float64) float64
}

func (sampler *quantileSampler) Sample(rng source.RNG) float64 {
	if sampler.bounds.Singleton() {
		return sampler.bounds.Lower
	}
	return sampler.bounds.Round(sampler.inverse(Fraction(rng)))
}

// NewQuantile prepares a monotone, closed-form conditional inverse. Endpoint
// probes include the smallest and largest possible midpoint fractions.
func NewQuantile(bounds Bounds, inverse func(float64) float64, conditionalCDF ...func(float64) float64) (source.FloatSampler, error) {
	if !bounds.Singleton() {
		for _, u := range []float64{1.0 / (1 << 53), 0.001, 0.25, 0.5, 0.75, 0.999, 1 - 1.0/(1<<53)} {
			value := inverse(u)
			slack := 8 * math.Max(math.Nextafter(bounds.Upper, math.Inf(1))-bounds.Upper, math.SmallestNonzeroFloat64)
			if !finite(value) || value < bounds.Lower-slack || value > bounds.Upper+slack {
				return nil, fmt.Errorf("randomness: conditional inverse is unavailable at probability %g", u)
			}
			if len(conditionalCDF) > 0 {
				actual := conditionalCDF[0](bounds.Round(value))
				if !finite(actual) || math.Abs(actual-u) > probabilityTolerance {
					return nil, fmt.Errorf("randomness: conditional quantiles are unresolved in float64")
				}
			}
		}
	}
	return &quantileSampler{bounds: bounds, inverse: inverse}, nil
}

// Distribution keeps CDF and survival separate to avoid upper-tail cancellation.
type Distribution struct {
	CDF, Survival, Quantile, InverseSurvival func(float64) float64
}
type conditionalSampler struct {
	bounds       Bounds
	distribution Distribution
	useSurvival  bool
	start, end   float64
}

// NewConditional chooses a stable tail orientation and prepares its inverse.
func NewConditional(bounds Bounds, distribution Distribution) (source.FloatSampler, error) {
	sampler := &conditionalSampler{bounds: bounds, distribution: distribution}
	if bounds.Singleton() {
		return sampler, nil
	}
	lowerCDF, upperCDF := distribution.CDF(bounds.Lower), distribution.CDF(bounds.Upper)
	lowerSurvival, upperSurvival := distribution.Survival(bounds.Lower), distribution.Survival(bounds.Upper)
	sampler.start, sampler.end = lowerCDF, upperCDF
	if upperCDF > 0.5 && lowerSurvival <= 0.5 {
		sampler.useSurvival = true
		sampler.start, sampler.end = lowerSurvival, upperSurvival
	}
	invalidStart := !finite(sampler.start) || sampler.start < 0 || sampler.start > 1
	invalidEnd := !finite(sampler.end) || sampler.end < 0 || sampler.end > 1
	invalidCDFOrder := !sampler.useSurvival && sampler.start > sampler.end
	invalidSurvivalOrder := sampler.useSurvival && sampler.start < sampler.end
	unresolvedMass := sampler.start == sampler.end

	if invalidStart || invalidEnd || invalidCDFOrder || invalidSurvivalOrder || unresolvedMass {
		return nil, fmt.Errorf("randomness: conditional probability mass is unresolved in float64")
	}

	// The retained probability interval must contain enough distinct probability values.
	// Otherwise a wide x interval could silently collapse into one sampled point.
	mass := math.Abs(sampler.end - sampler.start)
	startStep := math.Nextafter(sampler.start, math.Inf(1)) - sampler.start
	endStep := math.Nextafter(sampler.end, math.Inf(1)) - sampler.end
	if math.Max(startStep, endStep)/mass > probabilityTolerance {
		return nil, fmt.Errorf("randomness: conditional probability resolution is unavailable in float64")
	}

	// Reject inverses that cannot recover the requested conditional probability.
	for _, u := range []float64{1.0 / 1024, 0.01, 0.1, 0.5, 0.9, 0.99, 1023.0 / 1024} {
		target := sampler.probability(u)
		value := sampler.inverse(target)
		if !finite(value) || value < bounds.Lower || value >= bounds.Upper {
			value = sampler.bisect(target)
		}
		actual := distribution.CDF(value)
		if sampler.useSurvival {
			actual = distribution.Survival(value)
		}
		recoveredFraction := (actual - sampler.start) / (sampler.end - sampler.start)
		if !finite(actual) || math.Abs(recoveredFraction-u) > probabilityTolerance {
			return nil, fmt.Errorf("randomness: conditional quantiles are unresolved in float64")
		}
	}
	return sampler, nil
}

func (sampler *conditionalSampler) probability(u float64) float64 {
	value := (1-u)*sampler.start + u*sampler.end
	if value <= 0 {
		return math.SmallestNonzeroFloat64
	}
	if value >= 1 {
		return math.Nextafter(1, 0)
	}
	return value
}

func (sampler *conditionalSampler) inverse(p float64) float64 {
	if sampler.useSurvival {
		return sampler.distribution.InverseSurvival(p)
	}
	return sampler.distribution.Quantile(p)
}

func (sampler *conditionalSampler) Sample(rng source.RNG) float64 {
	if sampler.bounds.Singleton() {
		return sampler.bounds.Lower
	}
	target := sampler.probability(Fraction(rng))
	value := sampler.inverse(target)
	if finite(value) && value >= sampler.bounds.Lower && value < sampler.bounds.Upper {
		return value
	}
	return sampler.bisect(target)
}

func (sampler *conditionalSampler) bisect(target float64) float64 {
	lower, upper := sampler.bounds.Lower, sampler.bounds.Upper
	// 2200 halvings cover binary64's entire finite exponent range plus its
	// significant bits, even when the desired quantile is subnormal.
	for iteration := 0; iteration < 2200; iteration++ {
		middle := lower + (upper-lower)/2
		if middle == lower || middle == upper {
			return sampler.bounds.Round(lower)
		}
		probability := sampler.distribution.CDF(middle)
		if sampler.useSurvival {
			probability = sampler.distribution.Survival(middle)
		}
		below := probability < target
		if sampler.useSurvival {
			below = probability > target
		}
		if below {
			lower = middle
		} else {
			upper = middle
		}
	}
	// Unreachable for finite nonnegative bounds: adjacent floats were found.
	panic("randomness: finite binary64 bisection did not converge")
}

type integerFloor struct{ sampler source.FloatSampler }

func (sampler integerFloor) Sample(rng source.RNG) int64 {
	return int64(math.Floor(sampler.sampler.Sample(rng)))
}

type fixedInteger int64

func (value fixedInteger) Sample(source.RNG) int64 {
	return int64(value)
}

// Floor realizes whole units once and skips draws for a deterministic bucket.
func Floor(definition profile.Profile, sampler source.FloatSampler) source.IntegerSampler {
	minimum, _ := profile.ParseDecimal(*definition.Min)
	maximum, _ := profile.ParseDecimal(*definition.Max)
	first := new(big.Int).Quo(minimum.Num(), minimum.Denom())
	last := new(big.Int).Sub(maximum.Num(), big.NewInt(1))
	last.Quo(last, maximum.Denom())
	if first.Cmp(last) == 0 {
		return fixedInteger(first.Int64())
	}
	return integerFloor{sampler}
}

type fixedFloat float64

func (value fixedFloat) Sample(source.RNG) float64 {
	return float64(value)
}

// ConstantFloat requires an exactly representable deterministic floating value.
func ConstantFloat(definition profile.Profile) (source.FloatSampler, error) {
	if err := Check(definition, "constant"); err != nil {
		return nil, err
	}
	exact, _ := profile.ParseDecimal(*definition.Value)
	value, ok := exact.Float64()
	if !ok {
		return nil, fmt.Errorf("randomness: constant is not exactly representable as float64")
	}
	return fixedFloat(value), nil
}
