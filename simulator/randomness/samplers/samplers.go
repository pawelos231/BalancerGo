// Package samplers selects prepared samplers without selecting or consuming an RNG.
package samplers

import (
	"fmt"

	"load-balancer/randomness/exponential"
	"load-balancer/randomness/gamma"
	"load-balancer/randomness/internal/numeric"
	"load-balancer/randomness/normal"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
	"load-balancer/randomness/uniform"
)

// NewFloat prepares the selected family without consuming randomness.
func NewFloat(definition profile.Profile) (source.FloatSampler, error) {
	switch definition.Kind {
	case profile.KindConstant:
		return numeric.ConstantFloat(definition)
	case profile.KindUniform:
		return uniform.NewUniformFloatSampler(definition)
	case profile.KindNormal:
		return normal.New(definition)
	case profile.KindExponential:
		return exponential.New(definition)
	case profile.KindGamma:
		return gamma.New(definition)
	default:
		return nil, fmt.Errorf("randomness: no sampler for profile kind %q", definition.Kind)
	}
}

// NewInteger preserves exact discrete outcomes and floors other continuous samples once.
func NewInteger(definition profile.Profile) (source.IntegerSampler, error) {
	switch definition.Kind {
	case profile.KindConstant, profile.KindUniform:
		return uniform.NewIntegerSampler(definition)

	default:
		sampler, err := NewFloat(definition)
		if err != nil {
			return nil, err
		}
		return numeric.Floor(definition, sampler), nil
	}
}
