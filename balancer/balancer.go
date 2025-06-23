package balancer

import (
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/model"
	"load-balancer/server"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// HalfOpenTimeout defines the idle timeout for connections that have not completed the 3-way handshake.
	HalfOpenTimeout = 10 * time.Second
	// EstablishedTimeout defines the idle timeout for established connections with no traffic.
	EstablishedTimeout = 180 * time.Second
	// CleanupInterval specifies how often the idle connection manager should run.
	CleanupInterval = 5 * time.Second
)

type LoadBalancer struct {
	mu                 sync.Mutex                          // mutex to protect shared state
	Mode               model.LBMode                        // NAT / SNAT / DSR
	servers            []*server.Server                    // list of registered servers
	connTable          map[*server.Server][]*client.Client // connection table mapping servers to their clients
	clientLastActivity map[model.ClientKey]time.Time       // tracks the last activity time for each client
	algo               algorithms.Algorithm                // load balancing algorithm to use ex: "RoundRobin", "LeastConnections", etc.
	RefreshInterval    time.Duration                       // interval for getting the state of the load balancer

	// Channels for packet routing, they are treated as intermediary buffers
	// between clients and servers to handle incoming and outgoing packets.
	clientTx map[model.ClientKey]chan model.Packet // outgoing packets to clients, indexed by client key
	toSrv    map[*server.Server]chan model.Packet  // channel for packets to be sent to servers
	fromSrv  map[*server.Server]chan model.Packet  // channel for packets received from servers

	// Health check channels
	probe    map[*server.Server]chan struct{} // channel for health probes to servers
	probeAck map[*server.Server]chan struct{} // channel for health probe acknowledgments

	// Chaos engineering hooks
	chaos *model.ChaosConfig
}

func NewLoadBalancer(mode model.LBMode, algo algorithms.Algorithm, refreshInterval time.Duration) *LoadBalancer {
	lb := &LoadBalancer{
		RefreshInterval:    refreshInterval, // default refresh interval
		Mode:               mode,
		servers:            make([]*server.Server, 0),
		connTable:          make(map[*server.Server][]*client.Client),
		clientLastActivity: make(map[model.ClientKey]time.Time),
		algo:               algo,
		// initialize channels
		clientTx: make(map[model.ClientKey]chan model.Packet),
		toSrv:    make(map[*server.Server]chan model.Packet),
		fromSrv:  make(map[*server.Server]chan model.Packet),
		// initialize health check channels
		probe:    make(map[*server.Server]chan struct{}),
		probeAck: make(map[*server.Server]chan struct{}),
		chaos:    &model.ChaosConfig{},
	}

	go lb.startIdleConnectionManager()

	return lb
}

func (lb *LoadBalancer) AddClient(cl *client.Client) {
	lb.mu.Lock()
	if _, ok := lb.clientTx[cl.Key()]; !ok {
		lb.clientTx[cl.Key()] = cl.InChannel
	}
	lb.clientLastActivity[cl.Key()] = time.Now()
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
		lb.mu.Lock()
		lb.clientLastActivity[cl.Key()] = time.Now()
		lb.mu.Unlock()

		if lb.applyChaos() {
			continue
		}
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
		lb.safeSendToServer(srv, pkt)
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
		lb.mu.Lock()
		lb.clientLastActivity[pkt.Key] = time.Now()
		lb.mu.Unlock()

		if lb.applyChaos() {
			continue
		}
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

// SetChaosConfig updates the chaos engineering settings.
func (lb *LoadBalancer) SetChaosConfig(config *model.ChaosConfig) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.chaos = config
}

// ReviveServer simulates a server recovery for a given server ID.
func (lb *LoadBalancer) ReviveServer(serverID string) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for _, s := range lb.servers {
		if s.GetState().ID == serverID {
			s.MarkHealthy()
			return nil
		}
	}
	return fmt.Errorf("server with ID '%s' not found", serverID)
}

// KillRandomServer picks a random healthy server and marks it as failed.
// Returns the ID of the killed server, or an error if no healthy servers are available.
func (lb *LoadBalancer) KillRandomServer() (string, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	healthyServers := []*server.Server{}
	for _, s := range lb.servers {
		if s.IsHealthy() {
			healthyServers = append(healthyServers, s)
		}
	}

	if len(healthyServers) == 0 {
		return "", fmt.Errorf("no healthy servers to kill")
	}

	serverToKill := healthyServers[rand.Intn(len(healthyServers))]
	cl := lb.connTable[serverToKill]
	// first we need to close all clients connected to the server (not really real life scenario, but we CANNOT be sending packets to the closed channels)
	for _, client := range cl {
		client.Close() // close all clients connected to the killed server
	}
	serverToKill.Kill()

	return serverToKill.GetState().ID, nil
}

func (lb *LoadBalancer) getChaosConfig() model.ChaosConfig {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.chaos == nil {
		return model.ChaosConfig{}
	}
	return *lb.chaos
}

// applyChaos applies chaos engineering hooks to packet processing.
// It returns true if the packet should be dropped.
func (lb *LoadBalancer) applyChaos() bool {
	chaos := lb.getChaosConfig()

	// simulate latency
	if chaos.Latency > 0 {
		time.Sleep(chaos.Latency)
	}

	// simulate packet drop
	if chaos.PacketDropRate > 0 {
		if rand.Float64() < chaos.PacketDropRate {
			return true // drop packet
		}
	}

	return false
}

// startHealthChecker starts a goroutine that periodically checks the health of the server by sending a probe.
func (lb *LoadBalancer) startHealthChecker(srv *server.Server, interval time.Duration) {

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			{
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
		case <-srv.Done: // if server is done, stop health checking
			return
		}
	}

}

func (lb *LoadBalancer) Tick() model.LoadBalancerState {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	serversWithClients := make([]model.ServerStateWithClients, 0, len(lb.servers))
	for _, srv := range lb.servers {
		serverState := srv.GetState()

		clientStates := []model.ClientState{}
		if clients, ok := lb.connTable[srv]; ok {
			clientStates = make([]model.ClientState, len(clients))
			for i, cl := range clients {
				clientStates[i] = cl.GetState()
			}
		}

		serversWithClients = append(serversWithClients, model.ServerStateWithClients{
			Server:  serverState,
			Clients: clientStates,
		})
	}

	var algoName string
	if lb.algo != nil {
		algoName = reflect.TypeOf(lb.algo).Elem().Name()
	}

	return model.LoadBalancerState{
		Mode:      lb.Mode,
		Algorithm: algoName,
		Servers:   serversWithClients,
	}
}

func (lb *LoadBalancer) safeSendToServer(srv *server.Server, pkt model.Packet) {
	defer func() {
		if r := recover(); r != nil {
			// something bad happened, probably a send on a closed channel.
			// for now, we can just ignore it, but in a real-world scenario
			// this should be logged.
		}
	}()

	select {
	case lb.toSrv[srv] <- pkt:
		// packet sent successfully
	default:
		// channel is either full or closed, drop the packet
	}
}

// startIdleConnectionManager runs a background task to clean up idle connections.
func (lb *LoadBalancer) startIdleConnectionManager() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		lb.cleanupIdleConnections()
	}
}

// cleanupIdleConnections iterates through the connection table and removes clients that have been idle for too long.
func (lb *LoadBalancer) cleanupIdleConnections() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := time.Now()
	for srv, clients := range lb.connTable {
		survivingClients := make([]*client.Client, 0, len(clients))
		for _, cl := range clients {
			lastActivity, ok := lb.clientLastActivity[cl.Key()]
			if !ok {
				// This case should ideally not be reached if clientLastActivity is always populated when a client is added.
				// We'll keep the client if we don't have activity data, just in case.
				survivingClients = append(survivingClients, cl)
				continue
			}

			timeout := EstablishedTimeout
			if !cl.IsHandshakeCompleted() {
				timeout = HalfOpenTimeout
			}

			if now.Sub(lastActivity) > timeout {
				// Client has been idle for too long, close and remove it.
				fmt.Printf("INFO: Removing idle client %v (state: %s)\n", cl.Key(), map[bool]string{true: "ESTABLISHED", false: "SYN-RECV"}[cl.IsHandshakeCompleted()])
				cl.Close()
				delete(lb.clientTx, cl.Key())
				delete(lb.clientLastActivity, cl.Key())
			} else {
				survivingClients = append(survivingClients, cl)
			}
		}

		if len(survivingClients) < len(clients) {
			lb.connTable[srv] = survivingClients
		}
	}
}
