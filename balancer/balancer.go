package balancer

import (
	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/model"
	"load-balancer/server"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type LoadBalancer struct {
	mu              sync.Mutex                          // mutex to protect shared state
	Mode            model.LBMode                        // NAT / SNAT / DSR
	servers         []*server.Server                    // list of registered servers
	connTable       map[*server.Server][]*client.Client // connection table mapping servers to their clients
	algo            algorithms.Algorithm                // load balancing algorithm to use ex: "RoundRobin", "LeastConnections", etc.
	RefreshInterval time.Duration                       // interval for getting the state of the load balancer

	// Channels for packet routing, they are treated as intermediary buffers
	// between clients and servers to handle incoming and outgoing packets.
	clientTx map[model.ClientKey]chan model.Packet // outgoing packets to clients, indexed by client key
	toSrv    map[*server.Server]chan model.Packet  // channel for packets to be sent to servers
	fromSrv  map[*server.Server]chan model.Packet  // channel for packets received from servers

	// Health check channels
	probe    map[*server.Server]chan struct{} // channel for health probes to servers
	probeAck map[*server.Server]chan struct{} // channel for health probe acknowledgments
}

func NewLoadBalancer(mode model.LBMode, algo algorithms.Algorithm) *LoadBalancer {
	return &LoadBalancer{
		RefreshInterval: 1 * time.Second, // default refresh interval
		Mode:            mode,
		servers:         make([]*server.Server, 0),
		connTable:       make(map[*server.Server][]*client.Client),
		algo:            algo,
		// initialize channels
		clientTx: make(map[model.ClientKey]chan model.Packet),
		toSrv:    make(map[*server.Server]chan model.Packet),
		fromSrv:  make(map[*server.Server]chan model.Packet),
		// initialize health check channels
		probe:    make(map[*server.Server]chan struct{}),
		probeAck: make(map[*server.Server]chan struct{}),
	}
}

func (lb *LoadBalancer) AddClient(cl *client.Client) {
	lb.mu.Lock()
	if _, ok := lb.clientTx[cl.Key()]; !ok {
		lb.clientTx[cl.Key()] = cl.InChannel
	}
	lb.mu.Unlock()

	// start per-client listener
	// it listens for packets from the client and routes them to the appropriate server
	go lb.ListenOnClientEmission(cl)
}

// ListenOnClientEmission pulls packets from a single client,
// enforces the half-open backlog limit and forwards accepted packets
// to the chosen backend.
func (lb *LoadBalancer) ListenOnClientEmission(cl *client.Client) {
	for pkt := range cl.OutChannel {
		//fmt.Println("LB: received packet from client", cl.Key(), "with flags", pkt.Flag)

		// pick backend
		srv, mapped := lb.getServerByClientKey(pkt.Key)
		if !mapped {
			var err error
			srv, err = lb.Route(pkt, cl) // choose + persist in connTable

			if err != nil || srv == nil {
				continue // no backend available
			}
		}

		if pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK == 0 {
			// drop if backlog full
			if srv.MaxHalfOpen > 0 &&
				atomic.LoadInt32(&srv.HalfOpenConnections) >= srv.MaxHalfOpen {
				continue
			}
		}

		// forward packet to the server
		lb.toSrv[srv] <- pkt
	}
}

// AddServer registers a new server with the load balancer.
func (lb *LoadBalancer) AddServer(srv *server.Server) {
	lb.mu.Lock()
	// create per-backend channels
	lb.toSrv[srv] = make(chan model.Packet)   // outgoing packets to server
	lb.fromSrv[srv] = make(chan model.Packet) // incoming packets from server

	// create health check channels
	lb.probe[srv] = make(chan struct{}, 1)    // channel for health probes to server
	lb.probeAck[srv] = make(chan struct{}, 1) // channel for health probe acknowledgments

	// inject channels into server struct
	srv.DataIn = lb.toSrv[srv]
	srv.DataOut = lb.fromSrv[srv]
	srv.Probe = lb.probe[srv]
	srv.ProbeAck = lb.probeAck[srv]

	lb.servers = append(lb.servers, srv)
	lb.mu.Unlock()

	// start active health-checker
	go lb.backendRxLoop(srv)
	go lb.startHealthChecker(srv, 1*time.Second)
}

func (lb *LoadBalancer) backendRxLoop(srv *server.Server) {
	for pkt := range lb.fromSrv[srv] {
		// FOR DEBUGGING
		// fmt.Println("LB: received packet from server", srv.ID, "with flags", pkt.Flag, pkt.PlaceInStream, pkt.Key)

		// forward to client
		lb.mu.Lock()
		if tx := lb.clientTx[pkt.Key]; tx != nil {
			tx <- pkt
		}
		lb.mu.Unlock()
	}
}

// RemoveServer removes a server from the load balancer's list of registered servers.
func (lb *LoadBalancer) RemoveServer(srv *server.Server) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for i, s := range lb.servers {
		if s == srv {
			lb.servers = append(lb.servers[:i], lb.servers[i+1:]...)
			break
		}
	}
	delete(lb.connTable, srv)
}

// GetServers returns the list of registered servers in the load balancer.
func (lb *LoadBalancer) GetServers() []*server.Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.servers
}

// GetConnectionTable returns the current connection table mapping servers to their clients.
func (lb *LoadBalancer) GetConnectionTable() map[*server.Server][]*client.Client {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.connTable
}

// getClientByKey retrieves the server and client associated with the given client key.
func (lb *LoadBalancer) getServerByClientKey(key model.ClientKey) (*server.Server, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for srv, list := range lb.connTable {
		for _, cl := range list {
			if cl.Key() == key {
				return srv, true
			}
		}
	}
	return nil, false
}

// Route determines which server should handle the incoming packet based on the load balancing algorithm.
// ex: RoundRobin, LeastConnections, etc.
func (lb *LoadBalancer) Route(packet model.Packet, cl *client.Client) (*server.Server, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	srv, err := lb.algo.Pick(lb.servers)
	if err != nil || !srv.CanAcceptNewConnection() {
		return nil, err
	}

	lb.connTable[srv] = append(lb.connTable[srv], cl)
	return srv, nil
}

// ClearConnectionTable resets the connection table, removing all client-server mappings.
func (lb *LoadBalancer) ClearConnectionTable() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.connTable = make(map[*server.Server][]*client.Client)
}

// SetAlgorithm allows changing the load balancing algorithm at runtime.
func (lb *LoadBalancer) SetAlgorithm(algo algorithms.Algorithm) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.algo = algo
}

// GetAlgorithm returns the current load balancing algorithm.
func (lb *LoadBalancer) GetAlgorithm() algorithms.Algorithm {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.algo
}

// startHealthChecker starts a goroutine that periodically checks the health of the server by sending a probe.
func (lb *LoadBalancer) startHealthChecker(srv *server.Server, interval time.Duration) {

	t := time.NewTicker(interval)
	defer t.Stop()

	for range t.C {
		// send probe; non-blocking: if chan full we treat as immediate fail
		select {
		case srv.Probe <- struct{}{}:
		default:
			srv.MarkFailed()
			continue
		}

		// wait for ack with small timeout
		select {
		case <-srv.ProbeAck:
			if !srv.IsHealthy() {
				srv.MarkHealthy() // mark as healthy if ack received
			}
		case <-time.After(interval / 2):
			srv.MarkFailed()
		}
	}

}

func (lb *LoadBalancer) Tick() model.LoadBalancerState {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	serversAndClients := make(map[model.ServerState][]model.ClientState)
	for _, srv := range lb.servers {
		serverState := srv.GetState()

		clientStates := []model.ClientState{}
		if clients, ok := lb.connTable[srv]; ok {
			clientStates = make([]model.ClientState, len(clients))
			for i, cl := range clients {
				clientStates[i] = cl.GetState()
			}
		}
		serversAndClients[serverState] = clientStates
	}

	var algoName string
	if lb.algo != nil {
		algoName = reflect.TypeOf(lb.algo).Elem().Name()
	}

	return model.LoadBalancerState{
		Mode:              lb.Mode,
		Algorithm:         algoName,
		ServersAndClients: serversAndClients,
	}
}
