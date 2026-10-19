package algorithms

import (
	"errors"
	"load-balancer/server"
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

func (r *Random) Pick(servers []*server.Server) (*server.Server, error) {
	available := make([]*server.Server, 0, len(servers))
	for _, srv := range servers {
		if srv != nil && (srv.MaxConnections == 0 || srv.ActiveConnections < srv.MaxConnections) {
			available = append(available, srv)
		}
	}
	if len(available) == 0 {
		return nil, errors.New("no available backends")
	}
	if r.RNG == nil {
		// Preserve a usable zero value with a reproducible fallback seed.
		r.RNG = rand.New(rand.NewSource(1))
	}
	return available[r.RNG.Intn(len(available))], nil
}
