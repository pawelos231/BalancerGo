// Package poisson samples event counts conditioned on explicit integer bounds.
package poisson

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

const MaxSupportWidth = 65536

type Sampler struct {
	minimum    int64
	cumulative []float64
	total      float64
}

// NewInteger prepares whole outcomes without converting them through float64.
func NewInteger(definition profile.Profile) (source.IntegerSampler, error) {
	if err := numeric.Check(definition, "poisson"); err != nil {
		return nil, err
	}

	lower, _ := profile.ParseDecimal(*definition.Min)
	upper, _ := profile.ParseDecimal(*definition.Max)
	if !lower.IsInt() || !upper.IsInt() {
		return nil, fmt.Errorf("randomness: Poisson bounds must be integers")
	}
	if !lower.Num().IsInt64() || !upper.Num().IsInt64() {
		return nil, fmt.Errorf("randomness: Poisson bounds exceed int64")
	}
	minimum, maximum := lower.Num().Int64(), upper.Num().Int64()
	width := maximum - minimum
	if width <= 0 || width > MaxSupportWidth {
		return nil, fmt.Errorf("randomness: Poisson support width must be between 1 and %d", MaxSupportWidth)
	}
	mean, err := numeric.Field(definition.Mean, "mean", false)
	if err != nil {
		return nil, err
	}

	sampler := &Sampler{minimum: minimum}
	if mean == 0 {
		if minimum != 0 {
			return nil, fmt.Errorf("randomness: zero-mean Poisson truncation has zero probability")
		}
		sampler.cumulative = []float64{1}
		sampler.total = 1
		return sampler, nil
	}
	exactMean, _ := profile.ParseDecimal(*definition.Mean)
	modeBig := new(big.Int).Quo(exactMean.Num(), exactMean.Denom())
	mode := maximum - 1
	if modeBig.IsInt64() {
		mode = modeBig.Int64()
	}
	if mode < minimum {
		mode = minimum
	}
	if mode >= maximum {
		mode = maximum - 1
	}
	center := int(mode - minimum)
	// A mode weight of 1 avoids evaluating exp(-mean).
	weights := make([]float64, int(width))
	weights[center] = 1
	for i := center + 1; i < len(weights); i++ {
		weights[i] = weights[i-1] * (mean / float64(minimum+int64(i)))
	}
	for i := center - 1; i >= 0; i-- {
		weights[i] = weights[i+1] * (float64(minimum+int64(i)+1) / mean)
	}
	sampler.cumulative = make([]float64, len(weights))
	for i, weight := range weights {
		sampler.total += weight
		sampler.cumulative[i] = sampler.total
	}
	if math.IsNaN(sampler.total) || math.IsInf(sampler.total, 0) || sampler.total <= 0 {
		return nil, fmt.Errorf("randomness: Poisson probabilities are unresolved in float64")
	}
	return sampler, nil
}

func (sampler *Sampler) Sample(rng source.RNG) int64 {
	if len(sampler.cumulative) == 1 {
		return sampler.minimum
	}
	threshold := numeric.Fraction(rng) * sampler.total
	if threshold >= sampler.total {
		threshold = math.Nextafter(sampler.total, 0)
	}
	index := sort.Search(len(sampler.cumulative), func(i int) bool {
		return sampler.cumulative[i] > threshold
	})
	return sampler.minimum + int64(index)
}

type floating struct{ sampler source.IntegerSampler }

func (sampler floating) Sample(rng source.RNG) float64 {
	return float64(sampler.sampler.Sample(rng))
}

// New prepares floating Poisson outcomes only when every count is exactly representable.
func New(definition profile.Profile) (source.FloatSampler, error) {
	sampler, err := NewInteger(definition)
	if err != nil {
		return nil, err
	}

	minimum, _ := profile.ParseDecimal(*definition.Min)
	maximum, _ := profile.ParseDecimal(*definition.Max)
	first, last := minimum.Num().Int64(), maximum.Num().Int64()-1
	for value := first; ; value++ {
		exact := new(big.Rat).SetInt64(value)
		if _, ok := exact.Float64(); !ok {
			return nil, fmt.Errorf("randomness: Poisson support is not exactly representable as float64; use NewInteger")
		}
		if value == last {
			break
		}
	}
	return floating{sampler}, nil
}
