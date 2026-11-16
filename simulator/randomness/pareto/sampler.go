// Package pareto implements Type I Pareto with explicit finite truncation.
package pareto

import (
	"fmt"
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a Type I Pareto law conditioned above the declared lower bound.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "pareto"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	shape, err := numeric.Field(definition.Shape, "shape", true)
	if err != nil {
		return nil, err
	}

	mass := -math.Expm1(-shape * math.Log1p((bounds.Upper-bounds.Lower)/bounds.Lower))
	if mass <= 0 {
		return nil, fmt.Errorf("randomness: Pareto conditional mass is unresolved")
	}
	return numeric.NewQuantile(bounds, func(u float64) float64 {
		return bounds.Lower * math.Exp(-math.Log1p(-u*mass)/shape)
	}, func(x float64) float64 {
		return -math.Expm1(-shape*math.Log1p((x-bounds.Lower)/bounds.Lower)) / mass
	})
}
