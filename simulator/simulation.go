package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/engine"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
	"log"
	"math/rand"
	"time"
)

// Application wiring and optional wall-clock pacing live outside the engine.
// A simulation and every endpoint/link it creates have one owner: run's caller.
type simulation struct {
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

	publishState   func() error
	commandHandled func(error)

	scheduledClients      int
	createdClients        int
	completedStreams      int
	closedClients         int
	completedPayloadBytes uint64
	payload               payloadProgress
}

// namedLink adds topology labels to a physical link and its optional LB output.
// Managed outputs supply queue counters; incoming links keep their own FIFO.
type namedLink struct {
	ID     string
	Name   string
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

// newSimulation adapts the existing CLI and browser form to a scenario.
func newSimulation(cfg configuration) *simulation {
	if cfg.headless {
		cfg.speed = 0
	}

	resolved, err := scenario.Normalize(cfg.scenarioDefinition())
	if err != nil {
		panic(fmt.Errorf("simulation: invalid configuration: %w", err))
	}
	return newScenarioSimulation(resolved)
}

// newScenarioSimulation builds a runtime from a validated scenario kept read-only.
// Clients and their two links are created only when their arrival event runs.
func newScenarioSimulation(resolved scenario.Normalized) *simulation {
	execution := resolved.Execution
	e := engine.New(execution.Duration)
	e.MaxEvents = execution.MaxEvents

	s := &simulation{
		scenario: resolved,
		engine:   e,
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
	s.sampleTraffic()
	return s
}

func (s *simulation) applyCommand(command httpserver.Envelope) error {
	switch command.Type {
	case httpserver.SetAlgorithm:
		var msg httpserver.SetAlgorithmMsg
		if err := json.Unmarshal(command.Payload, &msg); err != nil {
			return err
		}
		switch msg.Name {
		case "roundRobin":
			s.lb.SetAlgorithm(&algorithms.RoundRobin{})
		case "random":
			s.lb.SetAlgorithm(&algorithms.Random{RNG: s.algorithmRNG})
		default:
			return fmt.Errorf("unknown algorithm: %s", msg.Name)
		}
	case httpserver.SpawnClient:
		var msg httpserver.SpawnClientMsg
		if err := json.Unmarshal(command.Payload, &msg); err != nil {
			return err
		}
		return s.spawnClients(msg)
	default:
		return fmt.Errorf("unsupported command: %s", command.Type)
	}
	return nil
}

func (s *simulation) run(ctx context.Context, commands <-chan httpserver.Envelope) error {
	wallStart := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := s.engine.Now()
		remaining := s.engine.EndTime() - next
		if remaining > s.scenario.Execution.SnapshotInterval {
			remaining = s.scenario.Execution.SnapshotInterval
		}
		next += remaining
		if s.scenario.Execution.Speed > 0 {
			// Pacing changes only real execution speed; protocol timers stay in DES.
			pacedTime := time.Duration(float64(next) / s.scenario.Execution.Speed)
			deadline := wallStart.Add(pacedTime)
			if delay := time.Until(deadline); delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		// Commands received while pacing become events at the current boundary,
		// before advancing. Even commands during the last wait are processed.
		// Limit draining to one queue batch; I/O never accesses the live models.
		for n := len(commands); n > 0; n-- {
			command := <-commands
			s.engine.ScheduleAfter(0, func() {
				err := s.applyCommand(command)
				if err != nil {
					log.Printf("command at %s: %v", s.engine.Now(), err)
				} else {
					log.Printf("command at %s: %s %s", s.engine.Now(), command.Type, command.Payload)
				}
				if s.commandHandled != nil {
					s.commandHandled(err)
				}
			})
		}
		err := s.advance(ctx, next)
		s.sampleTraffic()
		if err != nil {
			return err
		}
		// Publish after every event at this timestamp, including the final partial step.
		if s.publishState != nil {
			if err := s.publishState(); err != nil {
				return err
			}
		}
		if next == s.engine.EndTime() {
			break
		}
	}
	return nil
}

// advance polls application cancellation between bounded batches of callbacks,
// including when millions share a timestamp. RunUntil retains its queue on an
// event-limit error, so resuming does not alter virtual ordering or event times.
func (s *simulation) advance(ctx context.Context, until time.Duration) error {
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

type linkResult struct {
	Name   string
	Config network.Config
	network.Stats
}

// ingressQueueResult separates client and backend traffic at the shared receiver.
type ingressQueueResult struct {
	Side     string
	Capacity int
	network.Stats
}

// simulationResult reports completed work and unfinished queues at the end time.
// Forwarding counts packets once, when they start on any LB output.
type simulationResult struct {
	VirtualTime string
	WallTime    string
	Seed        int64

	ScheduledClients         int
	CreatedClients           int
	CompletedStreams         int
	ClosedClients            int
	IncompleteStreams        int
	CompletedPayloadBytes    uint64
	ReceivedPayloadBytes     uint64
	AcknowledgedPayloadBytes uint64
	Transfers                transferSummary

	EventsExecuted uint64
	PendingEvents  int
	Ingress        linkResult
	IngressQueues  []ingressQueueResult
	Forwarding     network.ForwardingStats
	Links          []linkResult
	State          model.LoadBalancerState
}

func (s *simulation) result(elapsed time.Duration) simulationResult {
	links := make([]linkResult, 0, len(s.links))
	for _, link := range s.links {
		links = append(links, linkResult{
			Name:   link.Name,
			Config: link.Link.Config(),
			Stats:  link.Stats(),
		})
	}

	return simulationResult{
		VirtualTime: s.engine.Now().String(),
		WallTime:    elapsed.String(),
		Seed:        s.scenario.Execution.Seed,

		ScheduledClients:         s.scheduledClients,
		CreatedClients:           s.createdClients,
		CompletedStreams:         s.completedStreams,
		ClosedClients:            s.closedClients,
		IncompleteStreams:        s.createdClients - s.completedStreams,
		CompletedPayloadBytes:    s.completedPayloadBytes,
		ReceivedPayloadBytes:     s.payload.ReceivedBytes,
		AcknowledgedPayloadBytes: s.payload.AcknowledgedBytes,
		Transfers:                s.transferResults(),

		EventsExecuted: s.engine.EventsExecuted(),
		PendingEvents:  s.engine.Pending(),
		Ingress: linkResult{
			Name:   "Shared LB input",
			Config: s.ingress.Config(),
			Stats:  s.ingress.Stats(),
		},
		IngressQueues: []ingressQueueResult{
			{
				Side:     "clients",
				Capacity: s.ingress.Config().QueueCapacity,
				Stats:    s.clientIngress.Stats(),
			},
			{
				Side:     "backends",
				Capacity: s.ingress.Config().QueueCapacity,
				Stats:    s.backendIngress.Stats(),
			},
		},
		Forwarding: s.egress.Stats(),
		Links:      links,
		State:      s.lb.Tick(),
	}
}
