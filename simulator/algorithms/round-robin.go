package algorithms

import (
	"errors"
	"sync"
)

type RoundRobin struct {
	mu  sync.Mutex
	idx int
}

// Pick checks at most one full rotation, skipping candidates at their mapping limit.
// The LB filters unavailable backends before calling Pick.
func (rr *RoundRobin) Pick(candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("no backends")
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.idx %= len(candidates)
	for checked := 0; checked < len(candidates); checked++ {
		index := rr.idx
		candidate := candidates[index]
		rr.idx = (rr.idx + 1) % len(candidates)
		if candidate.MaxConnections == 0 || candidate.MappedConnections < candidate.MaxConnections {
			return index, nil
		}
	}
	return -1, errors.New("no available backends")
}
