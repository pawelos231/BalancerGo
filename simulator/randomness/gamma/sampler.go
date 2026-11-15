// Package gamma implements shape/scale Gamma; integer shape is Erlang.
package gamma

import (
	"math"

	"gonum.org/v1/gonum/mathext"

	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// New prepares a shape/scale Gamma law; integer shape gives Erlang.
func New(definition profile.Profile) (source.FloatSampler, error) {
	if err := numeric.Check(definition, "gamma"); err != nil {
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

	return numeric.NewConditional(bounds, numeric.Distribution{
		CDF: func(x float64) float64 {
			return mathext.GammaIncReg(shape, x/scale)
		},
		Survival: func(x float64) float64 {
			return mathext.GammaIncRegComp(shape, x/scale)
		},
		Quantile: func(p float64) float64 {
			return scale * inverseCDF(shape, p)
		},
		InverseSurvival: func(p float64) float64 {
			return scale * mathext.GammaIncRegCompInv(shape, p)
		},
	})
}

// inverseCDF avoids one verified numerical failure in pinned Gonum v0.16.0.
// Its lower-tail root finder multiplies residuals -p and .25-p to check the
// initial bracket. For tiny positive p the product can underflow to signed zero,
// which Gonum treats as a nonbounding interval and panics. Returning NaN marks
// only that inverse as unavailable; conditional sampling then uses bounded CDF
// bisection in both preparation and draws. Other failures are not intercepted.
func inverseCDF(shape, probability float64) float64 {
	usesLowerTailRoot := probability > 0 && probability < 0.25
	if usesLowerTailRoot {
		lowerResidual := -probability
		upperResidual := 0.25 - probability
		if lowerResidual*upperResidual >= 0 {
			return math.NaN()
		}
	}

	return mathext.GammaIncRegInv(shape, probability)
}
