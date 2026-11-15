// Package beta scales a Beta(alpha,beta) variable to the affine [min,max) support.
package beta

import (
	"gonum.org/v1/gonum/mathext"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares an affine Beta law over the declared half-open support.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "beta"); err != nil {
		return nil, err
	}

	bounds, err := numeric.ReadBounds(definition)
	if err != nil {
		return nil, err
	}

	alpha, err := numeric.Field(definition.Alpha, "alpha", true)
	if err != nil {
		return nil, err
	}

	beta, err := numeric.Field(definition.Beta, "beta", true)
	if err != nil {
		return nil, err
	}

	span := bounds.Upper - bounds.Lower
	return numeric.NewConditional(bounds, numeric.Distribution{
		CDF: func(x float64) float64 {
			return mathext.RegIncBeta(alpha, beta, (x-bounds.Lower)/span)
		},
		Survival: func(x float64) float64 {
			return mathext.RegIncBeta(beta, alpha, (bounds.Upper-x)/span)
		},
		Quantile: func(p float64) float64 {
			return bounds.Lower + span*mathext.InvRegIncBeta(alpha, beta, p)
		},
		InverseSurvival: func(p float64) float64 {
			return bounds.Upper - span*mathext.InvRegIncBeta(beta, alpha, p)
		},
	})
}
