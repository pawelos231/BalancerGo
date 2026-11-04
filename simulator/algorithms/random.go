package algorithms

import (
	"errors"
	"math/rand"
)

// Random uses a private experiment RNG rather than the process-global source.
// The simulation owner serializes Pick calls.
type Random struct {
	RNG *rand.Rand
}

func NewRandom(seed int64) *Random {
	return &Random{RNG: rand.New(rand.NewSource(seed))}
}

func (r *Random) Pick(candidates []Candidate) (int, error) {
	available := make([]int, 0, len(candidates))
	for index, candidate := range candidates {
		if candidate.MaxConnections == 0 || candidate.MappedConnections < candidate.MaxConnections {
			available = append(available, index)
		}
	}
	if len(available) == 0 {
		return -1, errors.New("no available backends")
	}
	if r.RNG == nil {
		// Preserve a usable zero value with a reproducible fallback seed.
		r.RNG = rand.New(rand.NewSource(1))
	}
	return available[r.RNG.Intn(len(available))], nil
}
