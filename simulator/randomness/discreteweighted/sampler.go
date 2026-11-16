// Package discreteweighted preserves exact nonnegative decimal weight ratios.
package discreteweighted

import (
	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

func valuesAndWeights(definition profile.Profile) ([]string, []string, error) {
	if err := numeric.Check(definition, "discreteWeighted"); err != nil {
		return nil, nil, err
	}
	values := make([]string, len(definition.Entries))
	weights := make([]string, len(definition.Entries))
	for index, entry := range definition.Entries {
		values[index] = entry.Value
		weights[index] = entry.Weight
	}
	return values, weights, nil
}

// New prepares exact weighted selection of exactly representable floating outcomes.
func New(definition profile.Profile) (source.FloatSampler, error) {
	values, weights, err := valuesAndWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedFloat(values, weights)
}

// NewInteger prepares whole outcomes without converting them through float64.
func NewInteger(definition profile.Profile) (source.IntegerSampler, error) {
	values, weights, err := valuesAndWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedInteger(values, weights)
}
