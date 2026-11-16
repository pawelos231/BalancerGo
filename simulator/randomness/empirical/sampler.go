// Package empirical resamples an observation index uniformly, without interpolation.
// Duplicate observations retain their multiplicity in the empirical distribution.
package empirical

import (
	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

func observationWeights(definition profile.Profile) ([]string, error) {
	if err := numeric.Check(definition, "empirical"); err != nil {
		return nil, err
	}

	weights := make([]string, len(definition.Observations))
	for i := range weights {
		weights[i] = "1"
	}
	return weights, nil
}

// New prepares equal-probability resampling of the recorded floating observations.
func New(definition profile.Profile) (source.FloatSampler, error) {
	weights, err := observationWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedFloat(definition.Observations, weights)
}

// NewInteger prepares whole outcomes without converting them through float64.
func NewInteger(definition profile.Profile) (source.IntegerSampler, error) {
	weights, err := observationWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedInteger(definition.Observations, weights)
}
