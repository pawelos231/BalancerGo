package algorithms

import (
	"errors"
	"load-balancer/server"
	"sync"
)

type RoundRobin struct {
	mu  sync.Mutex
	idx int
}

// Pick checks at most one full rotation. A full backend set returns an error
// rather than recurring indefinitely. Eligibility is otherwise filtered by LB.
func (rr *RoundRobin) Pick(backends []*server.Server) (*server.Server, error) {
	if len(backends) == 0 {
		return nil, errors.New("no backends")
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.idx %= len(backends)
	for checked := 0; checked < len(backends); checked++ {
		s := backends[rr.idx]
		rr.idx = (rr.idx + 1) % len(backends)
		if s != nil && (s.MaxConnections == 0 || s.ActiveConnections < s.MaxConnections) {
			return s, nil
		}
	}
	return nil, errors.New("no available backends")
}
