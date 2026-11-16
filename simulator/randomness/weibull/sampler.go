// Package weibull samples via the exponential cumulative-hazard transform.
package weibull

import (
	"fmt"
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a conditional Weibull law through its cumulative-hazard transform.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "weibull"); err != nil {
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

	scale, err := numeric.Field(definition.Scale, "scale", true)
	if err != nil {
		return nil, err
	}

	lower := math.Pow(bounds.Lower/scale, shape)
	upper := math.Pow(bounds.Upper/scale, shape)
	if math.IsInf(lower, 0) || math.IsInf(upper, 0) || math.IsNaN(lower) || math.IsNaN(upper) || lower >= upper {
		return nil, fmt.Errorf("randomness: Weibull transformed support is unresolved in float64")
	}
	mass := -math.Expm1(-(upper - lower))
	if mass <= 0 {
		return nil, fmt.Errorf("randomness: Weibull conditional mass is unresolved")
	}
	return numeric.NewQuantile(bounds, func(u float64) float64 {
		return scale * math.Pow(lower-math.Log1p(-u*mass), 1/shape)
	}, func(x float64) float64 {
		return -math.Expm1(-(math.Pow(x/scale, shape) - lower)) / mass
	})
}
