// Package randomness defines explicit stochastic profiles and resource streams.
package randomness

import "load-balancer/randomness/profile"

// Kind identifies the supported profile definition.
type Kind = profile.Kind

const (
	KindConstant         = profile.KindConstant
	KindUniform          = profile.KindUniform
	KindNormal           = profile.KindNormal
	KindExponential      = profile.KindExponential
	KindGamma            = profile.KindGamma
	KindStudentT         = profile.KindStudentT
	KindBeta             = profile.KindBeta
	KindLognormal        = profile.KindLognormal
	KindPareto           = profile.KindPareto
	KindWeibull          = profile.KindWeibull
	KindTriangular       = profile.KindTriangular
	KindBernoulli        = profile.KindBernoulli
	KindPoisson          = profile.KindPoisson
	KindDiscreteWeighted = profile.KindDiscreteWeighted
	KindEmpirical        = profile.KindEmpirical
)

// Unit is the canonical base unit of every quantity in a profile.
type Unit = profile.Unit

const (
	UnitNanoseconds   = profile.UnitNanoseconds
	UnitBytes         = profile.UnitBytes
	UnitBitsPerSecond = profile.UnitBitsPerSecond
	UnitNumber        = profile.UnitNumber
	UnitPercentage    = profile.UnitPercentage
)

// Profile preserves exact decimal quantities and parameter presence.
type Profile = profile.Profile

// WeightedEntry preserves an exact outcome and its relative weight.
type WeightedEntry = profile.WeightedEntry

// ValidateProfile checks the definition without drawing randomness.
func ValidateProfile(definition Profile) error {
	return profile.ValidateProfile(definition)
}
