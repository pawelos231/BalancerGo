package algorithms

import (
	"fmt"
	"load-balancer/server"
	"math/rand"
)

type Random struct{}

func (r *Random) Pick(servers []*server.Server) (*server.Server, error) {
	if len(servers) == 0 {
		fmt.Println("No servers available in Random picker")
		return nil, nil // or an error
	}
	index := rand.Intn(len(servers))
	srv := servers[index]
	if srv.MaxConnections > 0 && srv.ActiveConnections >= srv.MaxConnections {
		// we pick again, might lead to infinite recursion if all servers are at max capacity
		return r.Pick(servers)
	}
	return servers[index], nil
}
