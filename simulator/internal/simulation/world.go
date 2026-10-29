package simulation

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"time"

	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
	"load-balancer/server"
)

// World owns the protocol endpoints, network and virtual-time measurements.
// All access, including observation, belongs to one owner goroutine.
type World struct {
	scenario     scenario.Normalized
	engine       *engine.Engine
	lb           *balancer.LoadBalancer
	algorithmRNG *rand.Rand

	egress         *network.Egress
	ingress        *network.Ingress
	clientIngress  *network.Input
	backendIngress *network.Input
	links          []namedLink
	clientGroups   []runtimeClientGroup
	clients        []runtimeClient
	traffic        trafficSampler

	algorithm string

	scheduledClients      int
	createdClients        int
	completedStreams      int
	closedClients         int
	completedPayloadBytes uint64
	payload               PayloadProgress
}

// namedLink identifies a physical path and its optional LB output.
// Managed outputs supply queue counters; incoming links keep their own FIFO.
type namedLink struct {
	ID     string
	Source string
	Target string
	Link   *network.Link
	Output *network.Output
}

// Stats includes the queue owned by the LB when this is an outgoing link.
func (link namedLink) Stats() network.Stats {
	if link.Output != nil {
		return link.Output.Stats()
	}
	return link.Link.Stats()
}

// New builds a world from a validated scenario kept read-only.
// Clients and their two links are created only when their arrival event runs.
func New(resolved scenario.Normalized) *World {
	execution := resolved.Execution
	e := engine.New(execution.Duration)
	e.MaxEvents = execution.MaxEvents

	s := &World{
		scenario:  resolved,
		algorithm: resolved.LoadBalancer.Algorithm,
		engine:    e,
	}
	s.algorithmRNG = s.resourceRNG("algorithm")

	var algorithm algorithms.Algorithm = &algorithms.RoundRobin{}
	if resolved.LoadBalancer.Algorithm == "random" {
		algorithm = &algorithms.Random{RNG: s.algorithmRNG}
	}

	s.lb = balancer.NewLoadBalancer(
		e,
		model.LBModeNAT,
		algorithm,
		execution.SnapshotInterval,
		s.resourceRNG("balancer"),
	)
	s.egress = network.NewEgress(e, resolved.LoadBalancer.ForwardingBitsPerSecond)

	// Two input queues share one physical receiver before LB processing.
	s.connectIngress(resolved.LoadBalancer.Ingress)
	for _, backend := range resolved.Backends {
		s.addBackend(backend)
	}

	s.initializeClients()
	s.SampleTraffic()
	return s
}

// Advance polls application cancellation between bounded batches of callbacks,
// including when millions share a timestamp. RunUntil retains its queue on an
// event-limit error, so resuming does not alter virtual ordering or event times.
func (s *World) Advance(ctx context.Context, until time.Duration) error {
	const batchSize uint64 = 4096
	limit := s.engine.MaxEvents
	defer func() { s.engine.MaxEvents = limit }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.engine.MaxEvents = limit
		if limit-s.engine.EventsExecuted() > batchSize {
			s.engine.MaxEvents = s.engine.EventsExecuted() + batchSize
		}
		err := s.engine.RunUntil(until)
		if errors.Is(err, engine.ErrEventLimit) && s.engine.EventsExecuted() < limit {
			continue
		}
		return err
	}
}

// resourceRNG gives each resource its own repeatable random sequence.
// Creating other links or reading snapshots cannot change this sequence.
func (s *World) resourceRNG(id string) *rand.Rand {
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(s.scenario.Execution.Seed))

	hash := fnv.New64a()
	_, _ = hash.Write(seed[:])
	_, _ = hash.Write([]byte(id))
	return rand.New(rand.NewSource(int64(hash.Sum64())))
}

// addLink creates one directed path with its own settings, FIFO and RNG.
func (s *World) addLink(
	id, source, target string,
	config scenario.LinkConfig,
) *network.Link {
	link := network.NewLink(s.engine, configuredLink(config), s.resourceRNG(id))

	s.links = append(s.links, namedLink{
		ID:     id,
		Source: source,
		Target: target,
		Link:   link,
	})
	return link
}

// configuredLink copies the scenario's link settings into the network model.
func configuredLink(config scenario.LinkConfig) network.Config {
	return network.Config{
		BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
		PacketTravelTime:       config.PacketTravelTime,
		QueueCapacity:          config.QueueCapacity,
		LossRate:               config.LossRate,
	}
}

// connectIngress creates two waiting queues served by one shared input link.
func (s *World) connectIngress(config scenario.LinkConfig) {
	link := network.NewLink(
		s.engine,
		configuredLink(config),
		s.resourceRNG("link/infrastructure/ingress"),
	)
	s.ingress = network.NewIngress(link)
	s.clientIngress = s.ingress.AddInput()
	s.backendIngress = s.ingress.AddInput()
}

// addOutput registers one directed path in the shared LB dispatcher.
// Its configured queue belongs to Output; the physical link has no second FIFO.
func (s *World) addOutput(
	id, source, target string,
	config scenario.LinkConfig,
) *network.Output {
	link := s.addLink(id, source, target, config)
	output := s.egress.AddOutput(link)
	s.links[len(s.links)-1].Output = output
	return output
}

// addBackend connects one configured backend to the shared input and dispatcher.
func (s *World) addBackend(config scenario.NormalizedBackend) {
	srv := server.NewServer(
		s.engine,
		config.ID,
		config.Name,
		config.Address,
		config.Port,
		0,   // Distance.
		1,   // Initial weight.
		100, // Half-open connection limit.
	)
	srv.SetReceiveBufferBytes(s.scenario.TCP.ReceiveBufferBytes)
	endpointID := BackendNodeID(config.ID)
	// Keep received bytes after this backend's connections close or it is removed.
	srv.OnPayloadReceived = func(bytes int) {
		s.payload.ReceivedBytes += uint64(bytes)
	}

	toBackend := s.addOutput(
		"link/"+endpointID+"/from-lb",
		LoadBalancerNodeID,
		endpointID,
		config.Network.FromLoadBalancer,
	)
	toInput := s.addLink(
		"link/"+endpointID+"/to-lb",
		endpointID,
		LoadBalancerNodeID,
		config.Network.ToLoadBalancer,
	)

	// A backend reply crosses its return link and then the common LB input.
	srv.SetSender(func(pkt model.Packet) {
		originalClient := s.lb.GetClient(pkt.Key)
		toInput.Transmit(pkt.WireSize(), func() {
			s.backendIngress.Transmit(pkt.WireSize(), func() {
				if s.lb.HasClient(originalClient) {
					s.lb.HandleServerPacket(srv, pkt)
				}
			})
		})
	})

	// Keep the original client identity while the packet waits or travels.
	err := s.lb.AddServer(srv, func(pkt model.Packet) {
		originalClient := s.lb.GetClient(pkt.Key)
		toBackend.Transmit(pkt.WireSize(), func() {
			if s.lb.HasClient(originalClient) {
				srv.HandlePacket(pkt)
			}
		})
	})

	if err != nil {
		panic(err)
	}

	srv.Start()
}

// SetAlgorithm switches selection without resetting the algorithm RNG.
func (s *World) SetAlgorithm(name string) error {
	switch name {
	case "roundRobin":
		s.lb.SetAlgorithm(&algorithms.RoundRobin{})
	case "random":
		s.lb.SetAlgorithm(&algorithms.Random{RNG: s.algorithmRNG})
	default:
		return fmt.Errorf("unknown algorithm: %s", name)
	}
	s.algorithm = name
	return nil
}

func (s *World) Now() time.Duration     { return s.engine.Now() }
func (s *World) EndTime() time.Duration { return s.engine.EndTime() }
func (s *World) EventsExecuted() uint64 { return s.engine.EventsExecuted() }
func (s *World) PendingEvents() int     { return s.engine.Pending() }
func (s *World) ScheduleAt(at time.Duration, callback func()) engine.EventID {
	return s.engine.ScheduleAt(at, callback)
}
func (s *World) ScheduleAfter(delay time.Duration, callback func()) engine.EventID {
	return s.engine.ScheduleAfter(delay, callback)
}

// AddBackend connects a backend using the same event and network wiring as New.
func (s *World) AddBackend(config scenario.NormalizedBackend) { s.addBackend(config) }

func (s *World) RemoveBackend(id string) error {
	for _, srv := range s.lb.GetServers() {
		if srv.ID == id {
			s.lb.RemoveServer(srv)
			return nil
		}
	}
	return fmt.Errorf("balancer: server %s not found", id)
}

func (s *World) MarkBackendFailed(id string) error {
	for _, srv := range s.lb.GetServers() {
		if srv.ID == id {
			srv.MarkFailed()
			return nil
		}
	}
	return fmt.Errorf("balancer: server %s not found", id)
}

func (s *World) ClearConnections() { s.lb.ClearConnectionTable() }

func (s *World) StopClient(id string) error {
	for _, runtime := range s.clients {
		if runtime.ID == id {
			runtime.Client.Shutdown()
			return nil
		}
	}
	return fmt.Errorf("simulation: client %s not found", id)
}
