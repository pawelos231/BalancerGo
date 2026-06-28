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
	HalfOpenTimeout = 5 * time.Second
	// EstablishedTimeout defines the idle timeout for established connections with no traffic.
	EstablishedTimeout = 50 * time.Second
	// CleanupInterval specifies how often the idle connection manager should run.
	CleanupInterval = 5 * time.Second
)

type LoadBalancer struct {
	mu                          sync.Mutex
	Mode                        model.LBMode
	servers                     []*server.Server
	connTable                   map[*server.Server][]*client.Client
	fastAccessClientKeyConTable map[model.ClientKey]*server.Server
	clientLastActivity          map[model.ClientKey]time.Time // do we need to keep it thread-safe? it is certain that given client key only one goroutine will update it
	algo                        algorithms.Algorithm
	RefreshInterval             time.Duration

	clientTx map[model.ClientKey]chan *model.Packet // outgoing packets to clients, indexed by client key
	toSrv    map[*server.Server]chan *model.Packet  // channel for packets to be sent to servers
	fromSrv  map[*server.Server]chan *model.Packet  // channel for packets received from servers

	// Health check channels
	probe    map[*server.Server]chan struct{}
	probeAck map[*server.Server]chan struct{}

	chaos *model.ChaosConfig
	wg    sync.WaitGroup
}

func NewLoadBalancer(mode model.LBMode, algo algorithms.Algorithm, refreshInterval time.Duration) *LoadBalancer {
	lb := &LoadBalancer{
		RefreshInterval:             refreshInterval,
		Mode:                        mode,
		servers:                     make([]*server.Server, 0),
		connTable:                   make(map[*server.Server][]*client.Client),
		fastAccessClientKeyConTable: make(map[model.ClientKey]*server.Server),
		clientLastActivity:          make(map[model.ClientKey]time.Time),
		algo:                        algo,
		clientTx:                    make(map[model.ClientKey]chan *model.Packet),
		toSrv:                       make(map[*server.Server]chan *model.Packet),
		fromSrv:                     make(map[*server.Server]chan *model.Packet),
		probe:                       make(map[*server.Server]chan struct{}),
		probeAck:                    make(map[*server.Server]chan struct{}),
		chaos:                       &model.ChaosConfig{},
		wg:                          sync.WaitGroup{},
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
	go lb.ListenOnClientEmission(cl)
}

func (lb *LoadBalancer) ListenOnClientEmission(cl *client.Client) {

	for {
		select {
		case pkt, ok := <-cl.OutChannel:
			{
				if !ok {
					return
				}

				lb.mu.Lock()
				lb.clientLastActivity[cl.Key()] = time.Now()
				lb.mu.Unlock()

				// to implement chaos features
				// if lb.applyChaos() {
				// 	return
				// }

				srv, mapped := lb.getServerByClientKey(pkt.Key)
				// to route the packet to the server, we need to check if the client is already mapped to a server
				if !mapped && pkt.Flag&model.FlagSYN != 0 {
					var err error
					srv, err = lb.Route(cl)

					if err != nil || srv == nil {
						break
					}
				}

				if pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK == 0 {
					// drop if backlog full
					if srv.MaxHalfOpen > 0 &&
						atomic.LoadUint32(&srv.HalfOpenConnections) >= srv.MaxHalfOpen {
						break
					}
				}

				lb.sendClientPacketToServer(srv, pkt)
			}
		case <-cl.Ctx.Done():
			return

		}
	}
}

func (lb *LoadBalancer) AddServer(srv *server.Server) {
	lb.mu.Lock()

	toCh := make(chan *model.Packet)
	fromCh := make(chan *model.Packet)
	probeCh := make(chan struct{}, 1)
	probeAckCh := make(chan struct{}, 1)

	lb.toSrv[srv] = toCh
	lb.fromSrv[srv] = fromCh
	lb.probe[srv] = probeCh
	lb.probeAck[srv] = probeAckCh

	srv.DataIn = toCh
	srv.DataOut = fromCh
	srv.Probe = probeCh
	srv.ProbeAck = probeAckCh

	lb.servers = append(lb.servers, srv)
	lb.mu.Unlock()

	go lb.ListenOnServerEmission(fromCh)
	go lb.startHealthChecker(srv, time.Second)
}

func (lb *LoadBalancer) ListenOnServerEmission(fromCh <-chan *model.Packet) {
	for pkt := range fromCh {
		lb.mu.Lock()
		lb.clientLastActivity[pkt.Key] = time.Now()
		if tx := lb.clientTx[pkt.Key]; tx != nil {
			tx <- pkt
		}
		lb.mu.Unlock()

		if lb.applyChaos() {
			continue
		}
	}
}

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

func (lb *LoadBalancer) GetServers() []*server.Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.servers
}

func (lb *LoadBalancer) GetConnectionTable() map[*server.Server][]*client.Client {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.connTable
}

func (lb *LoadBalancer) getServerByClientKey(key model.ClientKey) (*server.Server, bool) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if srv, ok := lb.fastAccessClientKeyConTable[key]; ok {
		// fmt.Println("Fast access hit for client key:", key)
		return srv, true
	} else {
		for srv, list := range lb.connTable {
			for _, cl := range list {
				if cl.Key() == key {
					return srv, true
				}
			}
		}
	}
	return nil, false
}

// ex: RoundRobin, LeastConnections, etc.
func (lb *LoadBalancer) Route(cl *client.Client) (*server.Server, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	srv, err := lb.algo.Pick(lb.servers)
	if err != nil || !srv.CanAcceptNewConnection() {
		return nil, err
	}

	lb.connTable[srv] = append(lb.connTable[srv], cl)
	lb.fastAccessClientKeyConTable[cl.Key()] = srv
	return srv, nil
}

func (lb *LoadBalancer) ClearConnectionTable() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.connTable = make(map[*server.Server][]*client.Client)
	lb.fastAccessClientKeyConTable = make(map[model.ClientKey]*server.Server)
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

func (lb *LoadBalancer) SetChaosConfig(config *model.ChaosConfig) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.chaos = config
}

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

// KillRandomServer picks a random healthy server and simulates a crash.
func (lb *LoadBalancer) KillRandomServer() (string, error) {
	lb.mu.Lock()

	healthyServers := []*server.Server{}
	for _, s := range lb.servers {
		if s.IsHealthy() {
			healthyServers = append(healthyServers, s)
		}
	}

	if len(healthyServers) == 0 {
		lb.mu.Unlock()
		return "", fmt.Errorf("no healthy servers to kill")
	}

	serverToKill := healthyServers[rand.Intn(len(healthyServers))]
	serverID := serverToKill.GetState().ID
	fmt.Println("INFO: Killing server", serverID)

	// Get server-specific channels before unlocking.
	// We need them to close them after the lock is released.
	toSrvChan, toSrvOk := lb.toSrv[serverToKill]
	fromSrvChan, fromSrvOk := lb.fromSrv[serverToKill]

	// Mark the server as failed immediately. This prevents new connections from being routed to it
	// by algorithms that check IsHealthy().
	serverToKill.MarkFailed()

	lb.mu.Unlock()

	serverToKill.Shutdown()

	if toSrvOk {
		close(toSrvChan)
	}
	if fromSrvOk {
		close(fromSrvChan)
	}

	return serverID, nil
}

func (lb *LoadBalancer) KillRandomClient() (string, error) {
	lb.mu.Lock()

	// Collect all clients
	var allClients []*client.Client
	for _, clients := range lb.connTable {
		allClients = append(allClients, clients...)
	}

	if len(allClients) == 0 {
		return "NO ID", fmt.Errorf("no clients to kill")
	}

	clientToKill := allClients[rand.Intn(len(allClients))]
	fmt.Println("INFO: Killing client", clientToKill.Key())
	delete(lb.clientTx, clientToKill.Key())
	delete(lb.clientLastActivity, clientToKill.Key())

	for srv, clients := range lb.connTable {
		survivingClients := make([]*client.Client, 0, len(clients))
		for _, cl := range clients {
			if cl.Key() == clientToKill.Key() {
				atomic.AddUint32(&srv.ActiveConnections, ^uint32(0))
				continue
			}
			survivingClients = append(survivingClients, cl)
		}

		if len(survivingClients) < len(clients) {
			lb.connTable[srv] = survivingClients
		}

	}
	lb.mu.Unlock()

	clientToKill.Shutdown()

	close(clientToKill.OutChannel)
	close(clientToKill.InChannel)

	// not idiomatic way of doing things in golang, but i think in this case it is worth it
	return clientToKill.Key().SrcIP, nil
}

func (lb *LoadBalancer) getChaosConfig() model.ChaosConfig {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.chaos == nil {
		return model.ChaosConfig{}
	}
	return *lb.chaos
}

func (lb *LoadBalancer) applyChaos() bool {
	chaos := lb.getChaosConfig()

	if chaos.Latency > 0 {
		time.Sleep(chaos.Latency)
	}

	if chaos.PacketDropRate > 0 {
		if rand.Float64() < chaos.PacketDropRate {
			return true // drop packet
		}
	}

	return false
}

func (lb *LoadBalancer) startHealthChecker(srv *server.Server, interval time.Duration) {

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			{
				select {
				case srv.Probe <- struct{}{}:
				default:
					srv.MarkFailed()
					continue
				}
				select {
				case <-srv.ProbeAck:
					if !srv.IsHealthy() {
						srv.MarkHealthy()
					}
				case <-time.After(interval / 2):
					srv.MarkFailed()
				}
			}
		case <-srv.Ctx.Done():
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

func (lb *LoadBalancer) sendClientPacketToServer(srv *server.Server, pkt *model.Packet) {
	// fmt.Println(pkt.Key.SrcIP, " SENDING PACKET TO SERVER ", srv.ID)
	select {
	case lb.toSrv[srv] <- pkt:
	case <-srv.Ctx.Done():
		return
	default:
	}

}

func (lb *LoadBalancer) startIdleConnectionManager() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		lb.cleanupIdleConnections()
	}
}

func (lb *LoadBalancer) cleanupIdleConnections() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := time.Now()
	for srv, clients := range lb.connTable {
		survivingClients := make([]*client.Client, 0, len(clients))
		for _, cl := range clients {
			lastActivity, ok := lb.clientLastActivity[cl.Key()]
			if !ok {
				// We'll keep the client if we don't have activity data, just in case.
				survivingClients = append(survivingClients, cl)
				continue
			}

			timeout := EstablishedTimeout
			if !cl.IsHandshakeCompleted() {
				timeout = HalfOpenTimeout
			}

			// fmt.Println(lastActivity.Add(-timeout))
			if now.Sub(lastActivity) > timeout {
				fmt.Printf("Removing idle client %v (state: %s)\n", cl.Key(), map[bool]string{true: "ESTABLISHED", false: "NOT-ESTABLISHED - SYN-RECV"}[cl.IsHandshakeCompleted()])
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
