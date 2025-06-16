package balancer

import (
	"load-balancer/optimizers"
	"load-balancer/server"
	"sync"
)

type ClientKey struct {
	IP   string
	Port uint16
}

type LoadBalancer struct {
	mu        sync.Mutex
	servers   []*server.Server
	connTable map[ClientKey]*server.Server
	algo      optimizers.Algorithm
}
