// Package bernoulli samples an exact decimal probability and returns binary values.
package bernoulli

import (
	"math/big"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

func probabilityWeights(definition profile.Profile) ([]string, error) {
	if err := numeric.Check(definition, "bernoulli"); err != nil {
		return nil, err
	}

	probability, err := profile.ParseDecimal(*definition.Probability)
	if err != nil {
		return nil, err
	}
	// Use integer coordinates for the event and its complement.
	failureWeight := new(big.Int).Sub(probability.Denom(), probability.Num())
	return []string{failureWeight.String(), probability.Num().String()}, nil
}

// New prepares exact rational Bernoulli selection with floating binary outcomes.
func New(definition profile.Profile) (source.FloatSampler, error) {
	weights, err := probabilityWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedFloat([]string{"0", "1"}, weights)
}

// NewInteger prepares whole outcomes without converting them through float64.
func NewInteger(definition profile.Profile) (source.IntegerSampler, error) {
	weights, err := probabilityWeights(definition)
	if err != nil {
		return nil, err
	}
	return numeric.WeightedInteger([]string{"0", "1"}, weights)
}
