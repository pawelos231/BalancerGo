package randomness

import "load-balancer/randomness/uniform"

// UniformFloatSampler approximates a continuous uniform profile with float64 values.
type UniformFloatSampler = uniform.UniformFloatSampler

// NewUniformFloatSampler validates and snapshots bounds without consuming RNG.
func NewUniformFloatSampler(definition Profile) (*UniformFloatSampler, error) {
	return uniform.NewUniformFloatSampler(definition)
}
