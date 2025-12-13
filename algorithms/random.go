package algorithms

import (
	"load-balancer/server"
	"math/rand"
)

type Random struct{}

func (r *Random) Pick(servers []*server.Server) (*server.Server, error) {
	if len(servers) == 0 {
		return nil, nil // or an error
	}
	index := rand.Intn(len(servers))
	return servers[index], nil
}
