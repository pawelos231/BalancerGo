package randomness

import "load-balancer/randomness/uniform"

// IntegerSampler compiles a constant or floor of a continuous uniform value.
type IntegerSampler = uniform.IntegerSampler

// NewIntegerSampler validates and snapshots bounds without consuming RNG.
func NewIntegerSampler(definition Profile) (*IntegerSampler, error) {
	return uniform.NewIntegerSampler(definition)
}
