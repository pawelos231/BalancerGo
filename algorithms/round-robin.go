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

// Pick returns the next server (or error if slice empty).
func (rr *RoundRobin) Pick(backends []*server.Server) (*server.Server, error) {
	if len(backends) == 0 {
		return nil, errors.New("no backends")
	}

	rr.mu.Lock()
	s := backends[rr.idx%len(backends)]
	rr.idx++
	rr.mu.Unlock()

	return s, nil
}
