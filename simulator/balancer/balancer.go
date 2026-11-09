// Package balancer models L4 forwarding. Its methods belong to the engine's
// owner goroutine; HTTP handlers must enqueue commands instead of calling them.
package balancer

import (
	"errors"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/host"
	"load-balancer/model"
	"load-balancer/server"
	"math/rand"
	"time"
)

const (
	CleanupInterval = 5 * time.Second
	healthInterval  = time.Second
)

type Sender func(model.Packet)

type healthCheck struct {
	tick           engine.EventID
	timeout        engine.EventID
	serial         uint64
	healthy        bool
	draining       bool
	maxConnections uint32 // Backend capacity recorded by the LB at registration.
}

type LoadBalancer struct {
	engine      *engine.Engine
	processor   *host.Host
	Mode        model.LBMode
	algo        algorithms.Algorithm
	rng         *rand.Rand
	servers     []*server.Server
	clients     map[model.ClientKey]*client.Client
	clientOrder []*client.Client
	connTable   map[*server.Server][]*client.Client
	byKey       map[model.ClientKey]*server.Server
	connections map[model.ClientKey]*connectionState
	clientTx    map[model.ClientKey]Sender
	toServer    map[*server.Server]Sender
	health      map[*server.Server]*healthCheck
	chaos       model.ChaosConfig
	cleanup     engine.EventID
	closed      bool

	timeouts TimeoutConfig
}

func NewLoadBalancer(e *engine.Engine, mode model.LBMode, algo algorithms.Algorithm, rng *rand.Rand) *LoadBalancer {
	if e == nil || algo == nil || rng == nil {
		panic("balancer: engine, algorithm and RNG are required")
	}
	lb := &LoadBalancer{
		engine: e, Mode: mode, algo: algo, rng: rng,
		processor:   host.New(e, host.DefaultConfig()),
		clients:     make(map[model.ClientKey]*client.Client),
		connTable:   make(map[*server.Server][]*client.Client),
		byKey:       make(map[model.ClientKey]*server.Server),
		connections: make(map[model.ClientKey]*connectionState),
		clientTx:    make(map[model.ClientKey]Sender),
		toServer:    make(map[*server.Server]Sender),
		health:      make(map[*server.Server]*healthCheck),
		timeouts: TimeoutConfig{
			HalfOpen:    model.DefaultLBHalfOpenIdleTimeout,
			Established: model.DefaultLBEstablishedIdleTimeout,
			Closing:     model.DefaultLBClosingIdleTimeout,
			Reset:       model.DefaultLBResetIdleTimeout,
		},
	}
	lb.scheduleCleanup()
	return lb
}

func (lb *LoadBalancer) AddClient(cl *client.Client, send Sender) error {
	if lb.closed || cl == nil || cl.IsTerminated() || send == nil {
		return errors.New("balancer: cannot add client")
	}

	key := cl.Key()
	if lb.byKey[key] != nil {
		return fmt.Errorf("balancer: duplicate client key: %v", key)
	}

	previous := lb.clients[key]
	lb.clients[key], lb.clientTx[key] = cl, send
	if previous != nil {
		for i, existing := range lb.clientOrder {
			if existing == previous {
				lb.clientOrder[i] = cl
				return nil
			}
		}
	}
	lb.clientOrder = append(lb.clientOrder, cl)
	return nil
}

// The instance check prevents old deliveries from reaching a replacement client.
func (lb *LoadBalancer) HasClient(cl *client.Client) bool {
	return !lb.closed && cl != nil && lb.clients[cl.Key()] == cl
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
	check := &healthCheck{
		healthy:        true,
		draining:       srv.Draining,
		maxConnections: srv.MaxConnections,
	}
	lb.health[srv] = check
	lb.scheduleHealthCheck(srv, check)
	return nil
}

// SetBackendDraining controls new mappings without interrupting pinned traffic.
// Health remains the result of the most recent probe, independent of this policy.
func (lb *LoadBalancer) SetBackendDraining(srv *server.Server, draining bool) {
	check := lb.health[srv]
	if check == nil || check.draining == draining {
		return
	}
	check.draining = draining
	if draining {
		srv.StartDrain()
	} else {
		srv.StopDrain()
	}
}

// RetireServer removes a backend from new selection and stops its local work.
// Existing routes and their senders stay pinned until ordinary mapping cleanup.
func (lb *LoadBalancer) RetireServer(srv *server.Server) {
	check := lb.health[srv]
	if check == nil {
		return
	}
	lb.engine.Cancel(check.tick)
	lb.engine.Cancel(check.timeout)
	delete(lb.health, srv)
	for index, backend := range lb.servers {
		if backend == srv {
			copy(lb.servers[index:], lb.servers[index+1:])
			lb.servers[len(lb.servers)-1] = nil
			lb.servers = lb.servers[:len(lb.servers)-1]
			break
		}
	}
	srv.Shutdown()
	if len(lb.connTable[srv]) == 0 {
		delete(lb.connTable, srv)
		delete(lb.toServer, srv)
	}
}

func (lb *LoadBalancer) HandleClientPacket(pkt model.Packet) {
	if lb.closed {
		return
	}
	cl := lb.clients[pkt.Key]
	connection := lb.connections[pkt.Key]
	lb.processor.Submit(func() {
		if !lb.HasClient(cl) || (connection != nil && lb.connections[pkt.Key] != connection) {
			return
		}
		lb.processClientPacket(pkt)
	})
}

// processClientPacket changes routing state only after the host has finished its work.
func (lb *LoadBalancer) processClientPacket(pkt model.Packet) {
	cl := lb.clients[pkt.Key]
	if cl == nil || !lb.HasClient(cl) {
		return
	}
	srv := lb.byKey[pkt.Key]
	baseFlags := pkt.Flag &^ (model.FlagECE | model.FlagCWR)
	if srv == nil && baseFlags == model.FlagSYN && supportedMappingPacket(pkt, false) {
		var err error
		srv, err = lb.Route(cl)
		if err != nil {
			return
		} // The client's virtual timeout handles this.
	}
	if srv == nil {
		return
	}
	connection := lb.connections[pkt.Key]
	connection.observe(pkt, false, lb.engine.Now())

	lb.forward(lb.toServer[srv], pkt, func() bool {
		return lb.HasClient(cl) && lb.connections[pkt.Key] == connection
	})
}

func (lb *LoadBalancer) HandleServerPacket(srv *server.Server, pkt model.Packet) {
	if lb.closed || lb.health[srv] == nil {
		return
	}
	cl := lb.clients[pkt.Key]
	connection := lb.connections[pkt.Key]
	lb.processor.Submit(func() {
		if !lb.HasClient(cl) || lb.connections[pkt.Key] != connection {
			return
		}
		lb.processServerPacket(srv, pkt)
	})
}

func (lb *LoadBalancer) processServerPacket(srv *server.Server, pkt model.Packet) {
	cl := lb.clients[pkt.Key]
	if cl == nil || !lb.HasClient(cl) || lb.byKey[pkt.Key] != srv || lb.health[srv] == nil {
		return
	}
	connection := lb.connections[pkt.Key]
	connection.observe(pkt, true, lb.engine.Now())
	lb.forward(lb.clientTx[pkt.Key], pkt, func() bool {
		return lb.HasClient(cl) && lb.connections[pkt.Key] == connection
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
	// Candidates contain only LB knowledge. The matching backend stays at the
	// same index, so selection never needs a partially initialized server.
	candidates := make([]algorithms.Candidate, 0, len(lb.servers))
	backends := make([]*server.Server, 0, len(lb.servers))
	for _, srv := range lb.servers {
		check := lb.health[srv]
		if check.healthy && !check.draining {
			candidates = append(candidates, algorithms.Candidate{
				MaxConnections:    check.maxConnections,
				MappedConnections: uint32(len(lb.connTable[srv])),
			})
			backends = append(backends, srv)
		}
	}

	selected, err := lb.algo.Pick(candidates)
	if err != nil {
		return nil, err
	}
	if selected < 0 || selected >= len(backends) {
		return nil, errors.New("balancer: algorithm selected an unknown backend")
	}

	srv := backends[selected]
	lb.connTable[srv] = append(lb.connTable[srv], cl)
	lb.byKey[cl.Key()] = srv
	lb.connections[cl.Key()] = &connectionState{
		lastActivity: lb.engine.Now(),
		phase:        stateHalfOpen,
	}
	return srv, nil
}

// forgetRoute releases only the mapping owned by this LB.
func (lb *LoadBalancer) forgetRoute(key model.ClientKey) {
	cl := lb.clients[key]
	if srv := lb.byKey[key]; srv != nil {
		list := lb.connTable[srv]
		for i, entry := range list {
			if entry == cl {
				copy(list[i:], list[i+1:])
				list[len(list)-1] = nil
				lb.connTable[srv] = list[:len(list)-1]
				break
			}
		}
		if lb.health[srv] == nil && len(lb.connTable[srv]) == 0 {
			delete(lb.connTable, srv)
			delete(lb.toServer, srv)
		}
	}
	delete(lb.byKey, key)
	delete(lb.connections, key)
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

// ClearConnectionTable drops LB mappings while leaving endpoints and registrations intact.
func (lb *LoadBalancer) ClearConnectionTable() {
	clear(lb.connTable)
	clear(lb.byKey)
	clear(lb.connections)
}

// PreparedAlgorithm binds a selector before a configuration commit starts.
type PreparedAlgorithm struct {
	lb        *LoadBalancer
	algorithm algorithms.Algorithm
}

func (lb *LoadBalancer) PrepareAlgorithm(algo algorithms.Algorithm) PreparedAlgorithm {
	if algo == nil {
		panic("balancer: nil algorithm")
	}
	return PreparedAlgorithm{lb: lb, algorithm: algo}
}

// Apply affects future mappings without changing routes or calling the selector.
func (update PreparedAlgorithm) Apply() {
	update.lb.algo = update.algorithm
}

func (lb *LoadBalancer) SetAlgorithm(algo algorithms.Algorithm) {
	lb.PrepareAlgorithm(algo).Apply()
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

// scheduleCleanup checks idle mappings every five virtual seconds.
// Traffic arriving before a sweep can refresh a mapping past its nominal deadline.
func (lb *LoadBalancer) scheduleCleanup() {
	lb.cleanup = lb.engine.ScheduleAfter(CleanupInterval, func() {
		if lb.closed {
			return
		}
		// Stable ordering keeps timer actions independent of Go map iteration.
		for _, cl := range lb.clientOrder {
			key := cl.Key()
			if connection := lb.connections[key]; connection != nil && lb.expired(connection) {
				lb.forgetRoute(key)
			}
		}
		lb.scheduleCleanup()
	})
}

// Probes retain the old idealised path. They do not yet consume link capacity.
func (lb *LoadBalancer) scheduleHealthCheck(srv *server.Server, check *healthCheck) {
	check.tick = lb.engine.ScheduleAfter(healthInterval, func() {
		if lb.closed || lb.health[srv] != check {
			return
		}
		check.serial++
		serial := check.serial
		check.timeout = lb.engine.ScheduleAfter(healthInterval/2, func() {
			if lb.health[srv] == check && check.serial == serial {
				check.healthy = false
			}
		})
		srv.HandleProbe(func() {
			if lb.health[srv] == check && check.serial == serial {
				lb.engine.Cancel(check.timeout)
				check.healthy = true
			}
		})
		lb.scheduleHealthCheck(srv, check)
	})
}

// VisitBackends reads detached values in backend registration order.
func (lb *LoadBalancer) VisitBackends(visit func(model.ServerState)) {
	for _, srv := range lb.servers {
		state := srv.GetState()
		state.HealthTCP = lb.health[srv].healthy
		state.Draining = lb.health[srv].draining
		visit(state)
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
	lb.processor.Stop()
	lb.engine.Cancel(lb.cleanup)
	lb.ClearConnectionTable()
	for _, check := range lb.health {
		lb.engine.Cancel(check.tick)
		lb.engine.Cancel(check.timeout)
	}
	clear(lb.clients)
	clear(lb.clientTx)
	clear(lb.toServer)
	clear(lb.health)
	lb.clientOrder = nil
	lb.servers = nil
}
