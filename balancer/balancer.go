package balancer

import (
	"load-balancer/algorithms"
	"load-balancer/model"
	"load-balancer/server"
	"sync"
)

type LoadBalancer struct {
	mu        sync.Mutex                         // mutex to protect shared state
	Mode      model.LBMode                       // NAT / SNAT / DSR
	servers   []*server.Server                   // list of registered servers
	connTable map[model.ClientKey]*server.Server // connection table mapping client keys to servers
	algo      algorithms.Algorithm               // load balancing algorithm to use ex: "RoundRobin", "LeastConnections", etc.
}

func newLoadBalancer(mode model.LBMode, algo algorithms.Algorithm) *LoadBalancer {
	return &LoadBalancer{
		Mode:      mode,
		servers:   make([]*server.Server, 0),
		connTable: make(map[model.ClientKey]*server.Server),
		algo:      algo,
	}
}

func (lb *LoadBalancer) AddServer(srv *server.Server) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.servers = append(lb.servers, srv)
}

func (lb *LoadBalancer) RemoveServer(srv *server.Server) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for i, s := range lb.servers {
		if s == srv {
			lb.servers = append(lb.servers[:i], lb.servers[i+1:]...)
			for key, value := range lb.connTable {
				if value == srv {
					delete(lb.connTable, key)
				}
			}

			break
		}
	}
}

func (lb *LoadBalancer) GetServers() []*server.Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.servers
}

func (lb *LoadBalancer) GetConnectionTable() map[model.ClientKey]*server.Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.connTable
}

func (lb *LoadBalancer) Tick(key model.ClientKey) {

}

func (lb *LoadBalancer) getClientByKey(key model.ClientKey) (*server.Server, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	srv, exists := lb.connTable[key]
	return srv, exists
}

func (lb *LoadBalancer) Route(packet model.Packet) (*server.Server, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// Check if the packet is already in the connection table
	if srv, exists := lb.connTable[packet.Key]; exists {
		return srv, nil
	}

	// If not, select a server using the load balancing algorithm
	srv, err := lb.algo.Pick(lb.servers)
	if err != nil || !srv.CanAcceptNewConnection() {
		return nil, err
	}

	// Add the new connection to the connection table
	lb.connTable[packet.Key] = srv

	return srv, nil
}

func (lb *LoadBalancer) ClearConnectionTable() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.connTable = make(map[model.ClientKey]*server.Server)
}

func (lb *LoadBalancer) SetAlgorithm(algo algorithms.Algorithm) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.algo = algo
}

func (lb *LoadBalancer) GetAlgorithm() algorithms.Algorithm {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.algo
}
