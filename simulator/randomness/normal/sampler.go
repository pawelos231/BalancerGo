// Package normal implements explicitly truncated normal profiles.
package normal

import (
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a normal law conditioned on the explicit half-open bounds.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "normal"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	mean, err := numeric.Field(definition.Mean, "mean", false)
	if err != nil {
		return nil, err
	}

	stddev, err := numeric.Field(definition.Stddev, "stddev", true)
	if err != nil {
		return nil, err
	}
	return numeric.NewConditional(bounds, numeric.Distribution{
		CDF: func(x float64) float64 {
			return .5 * math.Erfc(-(x-mean)/stddev/math.Sqrt2)
		},
		Survival: func(x float64) float64 {
			return .5 * math.Erfc((x-mean)/stddev/math.Sqrt2)
		},
		Quantile: func(p float64) float64 {
			return mean - stddev*math.Sqrt2*math.Erfcinv(2*p)
		},
		InverseSurvival: func(p float64) float64 {
			return mean + stddev*math.Sqrt2*math.Erfcinv(2*p)
		},
	})
}
