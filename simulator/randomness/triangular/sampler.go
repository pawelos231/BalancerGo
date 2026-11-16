// Package triangular supports interior and endpoint modes.
package triangular

import (
	"math"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares an analytic triangular inverse, including endpoint modes.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "triangular"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	mode, err := numeric.Field(definition.Mode, "mode", false)
	if err != nil {
		return nil, err
	}

	span := bounds.Upper - bounds.Lower
	relative := (mode - bounds.Lower) / span
	// An exact endpoint can round just outside the inward floating support.
	if relative < 0 {
		relative = 0
	}
	if relative > 1 {
		relative = 1
	}
	return numeric.NewQuantile(bounds, func(u float64) float64 {
		if relative == 0 {
			return bounds.Lower + span*u/(1+math.Sqrt(1-u))
		}
		if u < relative {
			return bounds.Lower + span*math.Sqrt(u*relative)
		}
		return bounds.Lower + span*(1-math.Sqrt((1-u)*(1-relative)))
	}, func(x float64) float64 {
		y := (x - bounds.Lower) / span
		if relative == 0 {
			return y * (2 - y)
		}
		if y < relative {
			return y * y / relative
		}
		if relative == 1 {
			return y * y
		}
		return 1 - (1-y)*(1-y)/(1-relative)
	})
}
