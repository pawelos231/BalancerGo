// Package balancer models L4 forwarding. Its methods belong to the engine's
// owner goroutine; HTTP handlers must enqueue commands instead of calling them.
package balancer

import (
	"errors"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/server"
	"math/rand"
	"time"
)

const (
	HalfOpenTimeout    = model.DefaultLBHalfOpenIdleTimeout
	EstablishedTimeout = model.DefaultLBEstablishedIdleTimeout
	CleanupInterval    = 5 * time.Second
	healthInterval     = time.Second
)

type Sender func(model.Packet)

type healthCheck struct {
	tick    engine.EventID
	timeout engine.EventID
	serial  uint64
}

type LoadBalancer struct {
	engine          *engine.Engine
	Mode            model.LBMode
	RefreshInterval time.Duration
	algo            algorithms.Algorithm
	rng             *rand.Rand
	servers         []*server.Server
	clients         map[model.ClientKey]*client.Client
	clientOrder     []*client.Client
	connTable       map[*server.Server][]*client.Client
	byKey           map[model.ClientKey]*server.Server
	lastActivity    map[model.ClientKey]time.Duration
	clientTx        map[model.ClientKey]Sender
	toServer        map[*server.Server]Sender
	health          map[*server.Server]*healthCheck
	chaos           model.ChaosConfig
	cleanup         engine.EventID
	closed          bool

	halfOpenIdleTimeout    time.Duration
	establishedIdleTimeout time.Duration
}

func NewLoadBalancer(e *engine.Engine, mode model.LBMode, algo algorithms.Algorithm, refreshInterval time.Duration, rng *rand.Rand) *LoadBalancer {
	if e == nil || algo == nil || rng == nil || refreshInterval <= 0 {
		panic("balancer: engine, algorithm, RNG and positive refresh interval are required")
	}
	lb := &LoadBalancer{
		engine: e, Mode: mode, algo: algo, RefreshInterval: refreshInterval, rng: rng,
		clients:                make(map[model.ClientKey]*client.Client),
		connTable:              make(map[*server.Server][]*client.Client),
		byKey:                  make(map[model.ClientKey]*server.Server),
		lastActivity:           make(map[model.ClientKey]time.Duration),
		clientTx:               make(map[model.ClientKey]Sender),
		toServer:               make(map[*server.Server]Sender),
		health:                 make(map[*server.Server]*healthCheck),
		halfOpenIdleTimeout:    HalfOpenTimeout,
		establishedIdleTimeout: EstablishedTimeout,
	}
	lb.scheduleCleanup()
	return lb
}

// SetIdleTimeouts sets separate cleanup limits before any clients arrive.
// Zero disables that limit, so long retransmission waits can be studied alone.
func (lb *LoadBalancer) SetIdleTimeouts(halfOpen, established time.Duration) {
	if halfOpen < 0 || established < 0 {
		panic("balancer: idle timeouts must be nonnegative")
	}
	if len(lb.clients) != 0 {
		panic("balancer: idle timeouts must be configured before adding clients")
	}
	lb.halfOpenIdleTimeout = halfOpen
	lb.establishedIdleTimeout = established
}

func (lb *LoadBalancer) AddClient(cl *client.Client, send Sender) error {
	if lb.closed || cl == nil || cl.IsTerminated() || send == nil {
		return errors.New("balancer: cannot add client")
	}
	key := cl.Key()
	if _, exists := lb.clients[key]; exists {
		return fmt.Errorf("balancer: duplicate client key: %v", key)
	}
	lb.clients[key], lb.clientTx[key] = cl, send
	lb.lastActivity[key] = lb.engine.Now()
	lb.clientOrder = append(lb.clientOrder, cl)
	previous := cl.OnClose
	cl.OnClose = func() {
		if lb.clients[key] == cl {
			lb.forgetClient(key)
		}
		if previous != nil {
			previous()
		}
	}
	return nil
}

// The instance check prevents old deliveries from reaching a replacement client.
func (lb *LoadBalancer) HasClient(cl *client.Client) bool {
	return !lb.closed && cl != nil && lb.clients[cl.Key()] == cl && !cl.IsTerminated()
}

func (lb *LoadBalancer) GetClient(key model.ClientKey) *client.Client { return lb.clients[key] }

func (lb *LoadBalancer) AddServer(srv *server.Server, send Sender) error {
	if lb.closed || srv == nil || send == nil {
		return errors.New("balancer: cannot add server")
	}
	for _, existing := range lb.servers {
		if existing == srv || existing.ID == srv.ID {
			return fmt.Errorf("balancer: duplicate server: %s", srv.ID)
		}
	}
	lb.servers = append(lb.servers, srv)
	lb.toServer[srv] = send
	check := &healthCheck{}
	lb.health[srv] = check
	lb.scheduleHealthCheck(srv, check)
	return nil
}

func (lb *LoadBalancer) HandleClientPacket(pkt model.Packet) {
	cl := lb.clients[pkt.Key]
	if cl == nil || !lb.HasClient(cl) {
		return
	}
	lb.lastActivity[pkt.Key] = lb.engine.Now()
	srv := lb.byKey[pkt.Key]
	if srv == nil && pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK == 0 {
		var err error
		srv, err = lb.Route(cl)
		if err != nil {
			return
		} // The client's virtual timeout handles this.
	}
	if srv == nil || srv.Killed {
		return
	}
	lb.forward(lb.toServer[srv], pkt, func() bool {
		return lb.HasClient(cl) && lb.byKey[pkt.Key] == srv && !srv.Killed
	})
}

func (lb *LoadBalancer) HandleServerPacket(srv *server.Server, pkt model.Packet) {
	cl := lb.clients[pkt.Key]
	if cl == nil || !lb.HasClient(cl) || lb.byKey[pkt.Key] != srv || srv.Killed {
		return
	}
	lb.lastActivity[pkt.Key] = lb.engine.Now()
	lb.forward(lb.clientTx[pkt.Key], pkt, func() bool {
		return lb.HasClient(cl) && lb.byKey[pkt.Key] == srv && !srv.Killed
	})
}

// Chaos acts before delivery. Senders enqueue on links, never call endpoints.
func (lb *LoadBalancer) forward(send Sender, pkt model.Packet, valid func() bool) {
	if send == nil || lb.rng.Float64() < lb.chaos.PacketDropRate {
		return
	}
	if lb.chaos.Latency == 0 {
		send(pkt)
		return
	}
	lb.engine.ScheduleAfter(lb.chaos.Latency, func() {
		if valid() {
			send(pkt)
		}
	})
}

func (lb *LoadBalancer) Route(cl *client.Client) (*server.Server, error) {
	if mapped := lb.byKey[cl.Key()]; mapped != nil {
		return mapped, nil
	}
	eligible := make([]*server.Server, 0, len(lb.servers))
	for _, srv := range lb.servers {
		if srv.CanAcceptNewConnection() {
			eligible = append(eligible, srv)
		}
	}

	srv, err := lb.algo.Pick(eligible)
	if err != nil {
		return nil, err
	}
	if srv == nil {
		return nil, errors.New("balancer: no available backend")
	}

	lb.connTable[srv] = append(lb.connTable[srv], cl)
	lb.byKey[cl.Key()] = srv
	return srv, nil
}

func (lb *LoadBalancer) forgetClient(key model.ClientKey) {
	cl := lb.clients[key]
	if srv := lb.byKey[key]; srv != nil {
		srv.ForgetConnection(key)
		list := lb.connTable[srv]
		for i, entry := range list {
			if entry == cl {
				copy(list[i:], list[i+1:])
				list[len(list)-1] = nil
				lb.connTable[srv] = list[:len(list)-1]
				break
			}
		}
	}
	delete(lb.clients, key)
	delete(lb.byKey, key)
	delete(lb.clientTx, key)
	delete(lb.lastActivity, key)
}

func (lb *LoadBalancer) RemoveServer(srv *server.Server) {
	for _, cl := range append([]*client.Client(nil), lb.connTable[srv]...) {
		cl.ShutdownWithReason(model.TransferBackendRemoved)
	}
	if check := lb.health[srv]; check != nil {
		lb.engine.Cancel(check.tick)
		lb.engine.Cancel(check.timeout)
		delete(lb.health, srv)
	}
	for i, candidate := range lb.servers {
		if candidate == srv {
			lb.servers = append(lb.servers[:i], lb.servers[i+1:]...)
			break
		}
	}
	delete(lb.toServer, srv)
	delete(lb.connTable, srv)
	srv.Shutdown()
}

func (lb *LoadBalancer) GetServers() []*server.Server {
	return append([]*server.Server(nil), lb.servers...)
}

func (lb *LoadBalancer) GetConnectionTable() map[*server.Server][]*client.Client {
	result := make(map[*server.Server][]*client.Client, len(lb.connTable))
	for srv, clients := range lb.connTable {
		result[srv] = append([]*client.Client(nil), clients...)
	}
	return result
}

func (lb *LoadBalancer) ClearConnectionTable() {
	for _, cl := range lb.clientOrder {
		if lb.clients[cl.Key()] == cl {
			cl.Shutdown()
		}
	}
}

func (lb *LoadBalancer) SetAlgorithm(algo algorithms.Algorithm) {
	if algo == nil {
		panic("balancer: nil algorithm")
	}
	lb.algo = algo
}

func (lb *LoadBalancer) GetAlgorithm() algorithms.Algorithm { return lb.algo }

func (lb *LoadBalancer) SetChaosConfig(config *model.ChaosConfig) {
	if config == nil {
		lb.chaos = model.ChaosConfig{}
		return
	}
	if config.Latency < 0 || !(config.PacketDropRate >= 0 && config.PacketDropRate <= 1) {
		panic("balancer: invalid chaos configuration")
	}
	lb.chaos = *config
}

func (lb *LoadBalancer) ReviveServer(serverID string) error {
	for _, srv := range lb.servers {
		if srv.ID == serverID {
			if srv.Killed {
				return fmt.Errorf("balancer: server %s has been shut down", serverID)
			}
			srv.MarkHealthy()
			return nil
		}
	}
	return fmt.Errorf("balancer: server %s not found", serverID)
}

func (lb *LoadBalancer) KillRandomServer() (string, error) {
	var candidates []*server.Server
	for _, srv := range lb.servers {
		if srv.IsHealthy() {
			candidates = append(candidates, srv)
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("balancer: no healthy server")
	}
	srv := candidates[lb.rng.Intn(len(candidates))]
	srv.Shutdown()
	return srv.ID, nil
}

func (lb *LoadBalancer) KillRandomClient() (string, error) {
	var candidates []*client.Client
	for _, cl := range lb.clientOrder {
		if lb.HasClient(cl) {
			candidates = append(candidates, cl)
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("balancer: no client")
	}
	cl := candidates[lb.rng.Intn(len(candidates))]
	cl.Shutdown()
	return cl.Key().SrcIP, nil
}

func (lb *LoadBalancer) scheduleCleanup() {
	lb.cleanup = lb.engine.ScheduleAfter(CleanupInterval, func() {
		if lb.closed {
			return
		}
		// Stable ordering keeps timer actions independent of Go map iteration.
		remaining := lb.clientOrder[:0]
		for _, cl := range lb.clientOrder {
			key := cl.Key()
			if lb.clients[key] != cl {
				continue
			}
			timeout := lb.establishedIdleTimeout
			if !cl.IsHandshakeCompleted() {
				timeout = lb.halfOpenIdleTimeout
			}
			if timeout > 0 && lb.engine.Now()-lb.lastActivity[key] >= timeout {
				cl.ShutdownWithReason(model.TransferBalancerTimeout)
			} else {
				remaining = append(remaining, cl)
			}
		}
		clear(lb.clientOrder[len(remaining):])
		lb.clientOrder = remaining
		lb.scheduleCleanup()
	})
}

// Probes retain the old idealised path. They do not yet consume link capacity.
func (lb *LoadBalancer) scheduleHealthCheck(srv *server.Server, check *healthCheck) {
	check.tick = lb.engine.ScheduleAfter(healthInterval, func() {
		if lb.closed || lb.health[srv] != check || srv.Killed {
			return
		}
		check.serial++
		serial := check.serial
		check.timeout = lb.engine.ScheduleAfter(healthInterval/2, func() {
			if lb.health[srv] == check && check.serial == serial && !srv.Killed {
				srv.UpdateHealth(false, srv.LatencyP95Milli)
			}
		})
		srv.HandleProbe(func() {
			if lb.health[srv] == check && check.serial == serial && !srv.Killed {
				lb.engine.Cancel(check.timeout)
				srv.UpdateHealth(true, srv.LatencyP95Milli)
			}
		})
		lb.scheduleHealthCheck(srv, check)
	})
}

// VisitBackends reads detached values in backend registration order.
func (lb *LoadBalancer) VisitBackends(visit func(model.ServerState)) {
	for _, srv := range lb.servers {
		visit(srv.GetState())
	}
}

// VisitRoutes preserves backend order and each backend's connection order.
func (lb *LoadBalancer) VisitRoutes(visit func(string, model.ClientKey)) {
	for _, srv := range lb.servers {
		for _, cl := range lb.connTable[srv] {
			visit(srv.ID, cl.Key())
		}
	}
}

func (lb *LoadBalancer) Shutdown() {
	if lb.closed {
		return
	}
	lb.closed = true
	lb.engine.Cancel(lb.cleanup)
	lb.ClearConnectionTable()
	for _, srv := range append([]*server.Server(nil), lb.servers...) {
		lb.RemoveServer(srv)
	}
}
