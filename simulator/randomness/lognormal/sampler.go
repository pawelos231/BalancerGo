// Package lognormal uses the result-space median and dimensionless logStddev.
package lognormal

import (
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a conditional lognormal law from median and log-space deviation.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "lognormal"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	median, err := numeric.Field(definition.Median, "median", true)
	if err != nil {
		return nil, err
	}

	logStddev, err := numeric.Field(definition.LogStddev, "logStddev", true)
	if err != nil {
		return nil, err
	}
	logMedian := math.Log(median)
	return numeric.NewConditional(bounds, numeric.Distribution{
		CDF: func(x float64) float64 {
			if x <= 0 {
				return 0
			}
			return .5 * math.Erfc(-(math.Log(x)-logMedian)/logStddev/math.Sqrt2)
		},
		Survival: func(x float64) float64 {
			if x <= 0 {
				return 1
			}
			return .5 * math.Erfc((math.Log(x)-logMedian)/logStddev/math.Sqrt2)
		},
		Quantile: func(p float64) float64 {
			return math.Exp(logMedian - logStddev*math.Sqrt2*math.Erfcinv(2*p))
		},
		InverseSurvival: func(p float64) float64 {
			return math.Exp(logMedian + logStddev*math.Sqrt2*math.Erfcinv(2*p))
		},
	})
}
