// Package exponential samples exponential profiles within explicit bounds.
package exponential

import (
	"fmt"
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares the conditional exponential inverse using its mean parameter.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "exponential"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	mean, err := numeric.Field(definition.Mean, "mean", true)
	if err != nil {
		return nil, err
	}

	// Measure from min so distant tails do not underflow.
	mass := -math.Expm1(-(bounds.Upper - bounds.Lower) / mean)
	if mass <= 0 {
		return nil, fmt.Errorf("randomness: exponential conditional mass is unresolved")
	}
	return numeric.NewQuantile(bounds, func(u float64) float64 {
		return bounds.Lower - mean*math.Log1p(-u*mass)
	}, func(x float64) float64 {
		return -math.Expm1(-(x-bounds.Lower)/mean) / mass
	})
}
