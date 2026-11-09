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
	"load-balancer/host"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// World owns the protocol endpoints, network and virtual-time measurements.
// All access, including observation, belongs to one owner goroutine.
type World struct {
	fixed         scenario.NormalizedFixedConfig
	configuration scenario.NormalizedState
	engine        *engine.Engine
	timeline      *timelineRuntime
	lb            *balancer.LoadBalancer
	algorithmRNG  *rand.Rand

	egress           *network.Egress
	ingress          *network.Ingress
	clientIngress    *network.Input
	backendIngress   *network.Input
	links            []namedLink
	backends         map[string]*runtimeBackend
	clientGroups     map[string]*runtimeClientGroup
	clientGroupOrder []*runtimeClientGroup
	clients          []runtimeClient
	traffic          trafficSampler

	algorithm string

	scheduledClients      int
	createdClients        int
	completedStreams      int
	closedClients         int
	completedPayloadBytes uint64
	payload               PayloadProgress
	stopped               bool
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

// newWorld installs timeline reservations before scheduling any client births.
func newWorld(
	fixed scenario.NormalizedFixedConfig,
	initial scenario.NormalizedState,
	timeline *timelineRuntime,
	reservations []clientGroupReservation,
) *World {
	execution := fixed.Execution
	e := engine.New(execution.Duration)
	e.MaxEvents = execution.MaxEvents

	s := &World{
		fixed:         fixed,
		configuration: initial,
		timeline:      timeline,
		algorithm:     initial.LoadBalancer.Algorithm,
		engine:        e,
		backends:      make(map[string]*runtimeBackend, len(initial.Backends)),
	}
	s.algorithmRNG = s.resourceRNG("algorithm")

	var algorithm algorithms.Algorithm = &algorithms.RoundRobin{}
	if initial.LoadBalancer.Algorithm == "random" {
		algorithm = &algorithms.Random{RNG: s.algorithmRNG}
	}

	s.lb = balancer.NewLoadBalancer(
		e,
		model.LBModeNAT,
		algorithm,
		s.resourceRNG("balancer"),
	)
	s.lb.SetMappingTimeouts(configuredMappingTimeouts(initial.LoadBalancer))
	s.lb.SetHostConfig(configuredHost(initial.LoadBalancer.Host))
	s.egress = network.NewEgress(e, initial.LoadBalancer.ForwardingBitsPerSecond)

	// Two input queues share one physical receiver before LB processing.
	s.connectIngress(initial.LoadBalancer.Ingress)
	for _, backend := range initial.Backends {
		s.addBackend(backend.NormalizedBackend)
	}

	s.initializeClients(reservations)
	s.SampleTraffic()
	return s
}

// Advance executes events through until, checking cancellation between batches.
func (s *World) Advance(ctx context.Context, until time.Duration) error {
	for {
		reached, err := s.AdvanceBatch(ctx, until)
		if err != nil || reached {
			return err
		}
	}
}

// AdvanceBatch executes up to 4096 events, then gives control back to the caller.
// A false result means more events remain before until, including at the same time.
func (s *World) AdvanceBatch(ctx context.Context, until time.Duration) (bool, error) {
	if s.stopped {
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	const batchSize uint64 = 4096
	limit := s.engine.MaxEvents
	defer func() { s.engine.MaxEvents = limit }()
	executed := s.engine.EventsExecuted()
	if executed < limit && limit-executed > batchSize {
		s.engine.MaxEvents = executed + batchSize
	}

	err := s.engine.RunUntil(until)
	if errors.Is(err, engine.ErrEventLimit) && s.engine.EventsExecuted() < limit {
		return false, nil
	}
	return err == nil, err
}

// resourceRNG gives each resource its own repeatable random sequence.
// Creating other links or reading snapshots cannot change this sequence.
func (s *World) resourceRNG(id string) *rand.Rand {
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(s.fixed.Execution.Seed))

	hash := fnv.New64a()
	_, _ = hash.Write(seed[:])
	_, _ = hash.Write([]byte(id))
	return rand.New(rand.NewSource(int64(hash.Sum64())))
}

// Only probabilistic signaling needs another random source; tail drop adds none.
func (s *World) configureCongestionRNG(link *network.Link, id string, config scenario.LinkConfig) {
	probability := config.CongestionSignalProbability
	if config.QueuePolicy == "threshold" && probability > 0 && probability < 1 {
		link.SetCongestionRNG(s.resourceRNG(id + "/congestion"))
	}
}

// addLink creates one directed path with its own settings, FIFO and RNG.
func (s *World) addLink(
	id, source, target string,
	config scenario.LinkConfig,
) *network.Link {
	link := network.NewLink(s.engine, configuredLink(config), s.resourceRNG(id))
	s.configureCongestionRNG(link, id, config)

	s.links = append(s.links, namedLink{
		ID:     id,
		Source: source,
		Target: target,
		Link:   link,
	})
	return link
}

func configuredMappingTimeouts(config scenario.NormalizedLoadBalancerConfig) balancer.TimeoutConfig {
	return balancer.TimeoutConfig{
		HalfOpen:    config.HalfOpenIdleTimeout,
		Established: config.EstablishedIdleTimeout,
		Closing:     config.ClosingIdleTimeout,
		Reset:       config.ResetIdleTimeout,
	}
}

func configuredHost(config scenario.NormalizedHostConfig) host.Config {
	// Older direct AddBackend callers leave the newly added host field empty.
	if config.Cores == 0 && config.PacketProcessingTime == 0 && config.QueueCapacity == 0 {
		return host.DefaultConfig()
	}
	return host.Config{
		Cores:                config.Cores,
		PacketProcessingTime: config.PacketProcessingTime,
		QueueCapacity:        config.QueueCapacity,
	}
}

func configuredDelayedACK(config scenario.NormalizedDelayedACKConfig) model.DelayedACKConfig {
	return model.DelayedACKConfig{
		Enabled:        config.Enabled,
		SegmentsPerACK: config.SegmentsPerACK,
		MaxDelay:       config.MaxDelay,
	}
}

func configuredRTO(config scenario.NormalizedRTOConfig) model.RTOConfig {
	return model.RTOConfig{
		Mode: model.RTOMode(config.Mode), ExponentialBackoff: config.ExponentialBackoff,
		InitialTimeout: config.InitialTimeout, MinimumTimeout: config.MinimumTimeout,
		MaximumTimeout: config.MaximumTimeout,
	}
}

func configuredClose(config scenario.NormalizedCloseConfig) model.CloseConfig {
	return model.CloseConfig{
		MSL: config.MSL, FinWaitTimeout: config.FinWaitTimeout, MaxFINRetries: config.MaxFINRetries,
	}
}

// configuredLink copies the scenario's link settings into the network model.
func configuredLink(config scenario.LinkConfig) network.Config {
	return network.Config{
		BandwidthBitsPerSecond:      config.BandwidthBitsPerSecond,
		PacketTravelTime:            config.PacketTravelTime,
		QueueCapacity:               config.QueueCapacity,
		LossRate:                    config.LossRate,
		QueuePolicy:                 config.QueuePolicy,
		CongestionThresholdPercent:  config.CongestionThresholdPercent,
		CongestionSignalProbability: config.CongestionSignalProbability,
	}
}

// connectIngress creates two waiting queues served by one shared input link.
func (s *World) connectIngress(config scenario.LinkConfig) {
	link := network.NewLink(
		s.engine,
		configuredLink(config),
		s.resourceRNG("link/infrastructure/ingress"),
	)
	s.configureCongestionRNG(link, "link/infrastructure/ingress", config)
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

// SetAlgorithm rejects changes outside the admitted scenario timeline.
func (*World) SetAlgorithm(string) error { return ErrTimelineManualMutation }

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

// AddBackend connects a backend using the world's event and network wiring.
func (s *World) AddBackend(config scenario.NormalizedBackend) { s.addBackend(config) }

// StopBackend represents a silent crash; only this backend's local state is cleared.
func (s *World) StopBackend(id string) error {
	for _, srv := range s.lb.GetServers() {
		if srv.ID == id {
			srv.Shutdown()
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

// Stop is the explicit exception that shuts down every endpoint and discards network work.
func (s *World) Stop() {
	if s.stopped {
		return
	}
	s.stopped = true

	for _, runtime := range s.clients {
		runtime.Client.Shutdown()
	}
	for _, backend := range s.lb.GetServers() {
		backend.Shutdown()
	}
	s.lb.Shutdown()

	// Drop arrivals, queued packets and periodic callbacks together.
	s.engine.Clear()
	s.clientGroups = nil
	s.clientGroupOrder = nil
	for _, link := range s.links {
		if link.Output != nil {
			link.Output.DiscardPending()
		}
		link.Link.DiscardPending()
	}
	s.ingress.DiscardPending()
	s.egress.DiscardPending()
}
