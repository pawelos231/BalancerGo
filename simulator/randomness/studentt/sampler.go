// Package studentt implements location/scale Student t, conditionally truncated.
package studentt

import (
	"gonum.org/v1/gonum/stat/distuv"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a conditional Student t law from location, scale, and degrees of freedom.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "studentT"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	location, err := numeric.Field(definition.Location, "location", false)
	if err != nil {
		return nil, err
	}

	scale, err := numeric.Field(definition.Scale, "scale", true)
	if err != nil {
		return nil, err
	}

	degreesOfFreedom, err := numeric.Field(definition.DegreesOfFreedom, "degreesOfFreedom", true)
	if err != nil {
		return nil, err
	}

	distribution := distuv.StudentsT{Mu: location, Sigma: scale, Nu: degreesOfFreedom}
	return numeric.NewConditional(bounds, numeric.Distribution{
		CDF:      distribution.CDF,
		Survival: distribution.Survival,
		Quantile: distribution.Quantile,
		InverseSurvival: func(p float64) float64 {
			// Student t is symmetric around location.
			return 2*location - distribution.Quantile(p)
		},
	})
}
