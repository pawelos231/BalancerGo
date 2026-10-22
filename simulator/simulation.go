package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/client"
	"load-balancer/engine"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/server"
	"log"
	"math/rand"
	"time"
)

// Application wiring and optional wall-clock pacing live outside the engine.
// A simulation and every endpoint/link it creates have one owner: run's caller.
type simulation struct {
	config       configuration
	engine       *engine.Engine
	lb           *balancer.LoadBalancer
	algorithmRNG *rand.Rand

	egress   *network.Egress
	uplink   *network.Link
	downlink *network.Output
	links    []namedLink

	publishState   func() error
	commandHandled func(error)

	scheduledClients      int
	createdClients        int
	completedStreams      int
	closedClients         int
	completedPayloadBytes uint64
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

// newSimulation connects the endpoints and schedules the first model events.
func newSimulation(cfg configuration) *simulation {
	e := engine.New(cfg.duration)
	e.MaxEvents = cfg.maxEvents

	s := &simulation{
		config:       cfg,
		engine:       e,
		algorithmRNG: rand.New(rand.NewSource(cfg.seed + 2)),
	}

	// Backend selection and packet forwarding use separate random sources.
	var algo algorithms.Algorithm = &algorithms.RoundRobin{}
	if cfg.algorithm == "random" {
		algo = &algorithms.Random{RNG: s.algorithmRNG}
	}

	balancerRNG := rand.New(rand.NewSource(cfg.seed + 1))
	s.lb = balancer.NewLoadBalancer(
		e,
		model.LBModeNAT,
		algo,
		cfg.snapshotInterval,
		balancerRNG,
	)
	s.egress = network.NewEgress(e, cfg.forwardingBitsPerSecond)

	// Clients currently share one link in each direction.
	s.uplink = s.addLink("clients -> LB", "clients", "lb")
	s.downlink = s.addOutput("LB -> clients", "lb", "clients")

	for i := 0; i < cfg.servers; i++ {
		// Give each backend a stable identity and address.
		serverID := fmt.Sprintf("srv-%02d", i)
		serverName := fmt.Sprintf("backend-%02d", i)
		serverAddress := fmt.Sprintf("10.0.%d.%d", i/254, i%254+1)

		srv := server.NewServer(
			e,
			serverID,
			serverName,
			serverAddress,
			80,  // Port.
			0,   // Distance.
			1,   // Initial weight.
			100, // Half-open connection limit.
		)

		// The LB owns the outgoing FIFO; the return link owns its own queue.
		toBackend := s.addOutput("LB -> "+srv.ID, "lb", srv.ID)
		toBalancer := s.addLink(srv.ID+" -> LB", srv.ID, "lb")

		// Backend -> LB: ignore arrivals for clients that have already closed.
		srv.SetSender(func(pkt model.Packet) {
			client := s.lb.GetClient(pkt.Key)

			toBalancer.Transmit(pkt.WireSize(), func() {
				if s.lb.HasClient(client) {
					s.lb.HandleServerPacket(srv, pkt)
				}
			})
		})

		// LB -> backend: queue the packet and deliver it if its client is still active.
		err := s.lb.AddServer(srv, func(pkt model.Packet) {
			client := s.lb.GetClient(pkt.Key)

			toBackend.Transmit(pkt.WireSize(), func() {
				if s.lb.HasClient(client) {
					srv.HandlePacket(pkt)
				}
			})
		})
		if err != nil {
			panic(err)
		}

		srv.Start()
	}

	// Schedule client arrivals and the first snapshot at virtual time zero.
	s.scheduleClients(cfg.clients)
	e.ScheduleAt(0, s.sample)

	return s
}

// addLink creates an autonomous link with stable labels and a seeded RNG.
func (s *simulation) addLink(name, source, target string) *network.Link {
	link := network.NewLink(s.engine, network.Config{
		BandwidthBitsPerSecond: s.config.bandwidth,
		PacketTravelTime:       s.config.packetTravelTime,
		QueueCapacity:          s.config.queue,
		LossRate:               s.config.loss,
	}, rand.New(rand.NewSource(s.config.seed+100+int64(len(s.links)))))
	s.links = append(s.links, namedLink{
		ID:     fmt.Sprintf("link-%03d", len(s.links)),
		Name:   name,
		Source: source,
		Target: target,
		Link:   link,
	})
	return link
}

// addOutput gives the LB exclusive ownership of this outgoing link's FIFO.
func (s *simulation) addOutput(name, source, target string) *network.Output {
	link := s.addLink(name, source, target)
	output := s.egress.AddOutput(link)
	s.links[len(s.links)-1].Output = output
	return output
}

func (s *simulation) scheduleClients(count int) {
	s.scheduledClients += count
	var arrival func()
	arrival = func() {
		if count == 0 {
			return
		}
		s.startClient()
		count--
		if count > 0 {
			s.engine.ScheduleAfter(s.config.clientGap, arrival)
		}
	}
	if count > 0 {
		s.engine.ScheduleAfter(0, arrival)
	}
}

func (s *simulation) startClient() {
	id := s.createdClients + 1
	s.createdClients++
	cl := client.NewClient(s.engine,
		fmt.Sprintf("192.%d.%d.%d", byte(id>>16), byte(id>>8), byte(id)),
		uint16(40000+id%20000), "lb.local", 80, model.TCP)
	cl.SetSender(func(pkt model.Packet) {
		s.uplink.Transmit(pkt.WireSize(), func() {
			if s.lb.HasClient(cl) {
				s.lb.HandleClientPacket(pkt)
			}
		})
	})
	cl.OnStreamComplete = func() {
		s.completedStreams++
		s.completedPayloadBytes += uint64(s.config.streamBytes)
	}
	cl.OnClose = func() { s.closedClients++ }
	if err := s.lb.AddClient(cl, func(pkt model.Packet) {
		s.downlink.Transmit(pkt.WireSize(), func() {
			if s.lb.HasClient(cl) {
				cl.HandlePacket(pkt)
			}
		})
	}); err != nil {
		panic(err)
	}
	cl.SendCompleteStream(s.config.streamBytes, s.config.packetGap)
	cl.EstablishConnection()
}

func (s *simulation) sample() {
	if s.publishState != nil {
		if err := s.publishState(); err != nil {
			log.Printf("snapshot: %v", err)
		}
	}
	s.engine.ScheduleAfter(s.config.snapshotInterval, s.sample)
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
		if msg.Count < 1 || msg.Count > 10000 || s.scheduledClients > (1<<24-1)-msg.Count {
			return errors.New("spawn count must be 1..10000 and fit the client address range")
		}
		s.scheduleClients(msg.Count)
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
		if remaining > s.config.snapshotInterval {
			remaining = s.config.snapshotInterval
		}
		next += remaining
		if s.config.speed > 0 {
			// Pacing changes only real execution speed; protocol timers stay in DES.
			deadline := wallStart.Add(time.Duration(float64(next) / s.config.speed))
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
		if err := s.advance(ctx, next); err != nil {
			return err
		}
		if next == s.engine.EndTime() {
			break
		}
	}
	// A final frame is needed when the end time is not a multiple of the interval.
	if s.publishState != nil {
		return s.publishState()
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
	Name string
	network.Stats
}

// simulationResult reports completed work and unfinished queues at the end time.
// Forwarding counts packets once, when they start on any LB output.
type simulationResult struct {
	VirtualTime string
	WallTime    string
	Seed        int64

	ScheduledClients      int
	CreatedClients        int
	CompletedStreams      int
	ClosedClients         int
	IncompleteStreams     int
	CompletedPayloadBytes uint64

	EventsExecuted uint64
	PendingEvents  int
	Forwarding     network.ForwardingStats
	Links          []linkResult
	State          model.LoadBalancerState
}

func (s *simulation) result(elapsed time.Duration) simulationResult {
	links := make([]linkResult, 0, len(s.links))
	for _, link := range s.links {
		links = append(links, linkResult{
			Name:  link.Name,
			Stats: link.Stats(),
		})
	}

	return simulationResult{
		VirtualTime: s.engine.Now().String(),
		WallTime:    elapsed.String(),
		Seed:        s.config.seed,

		ScheduledClients:      s.scheduledClients,
		CreatedClients:        s.createdClients,
		CompletedStreams:      s.completedStreams,
		ClosedClients:         s.closedClients,
		IncompleteStreams:     s.createdClients - s.completedStreams,
		CompletedPayloadBytes: s.completedPayloadBytes,

		EventsExecuted: s.engine.EventsExecuted(),
		PendingEvents:  s.engine.Pending(),
		Forwarding:     s.egress.Stats(),
		Links:          links,
		State:          s.lb.Tick(),
	}
}
