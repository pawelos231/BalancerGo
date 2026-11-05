package server

import (
	"math"
	"sort"
	"time"

	"load-balancer/engine"
	"load-balancer/host"
	"load-balancer/model"
	"load-balancer/protocols/tcp"
)

// Server owns backend availability, admission limits and host observations.
// Its TCP receiver owns connection state, byte ranges and protocol timers.
type Server struct {
	ID              string
	Name            string
	Address         string
	Port            uint16
	Distance        int16
	Weight          int16
	Active          bool
	Killed          bool
	Tags            []string
	Metadata        map[string]string
	Draining        bool
	HealthTCP       bool
	LatencyP95Milli uint32

	MaxConnections      uint32
	ActiveConnections   uint32
	HalfOpenConnections uint32
	MaxHalfOpen         uint32
	DrainTimeout        time.Duration
	DrainStartedAt      time.Duration

	Version       string
	ConfigVersion string
	LastUpdated   time.Duration

	NumberOfHandledPackets uint32

	// Called only for new payload accepted by a connection, before its ACK is sent.
	OnPayloadReceived func(int)

	transport       *tcp.Receiver
	processor       *host.Host
	engine          *engine.Engine
	sender          func(model.Packet)
	started         bool
	probeEvents     map[engine.EventID]struct{}
	latencySamples  [100]int64
	latencyCount    int
	latencyPosition int
}

func NewServer(
	e *engine.Engine,
	id, name, address string,
	port uint16,
	distance, weight int16,
	maxHalfOpen uint32,
) *Server {
	if e == nil {
		panic("server: nil engine")
	}
	s := &Server{
		ID: id, Name: name, Address: address, Port: port,
		Distance: distance, Weight: weight, MaxHalfOpen: maxHalfOpen,
		Active: true, HealthTCP: true,
		Tags: []string{}, Metadata: make(map[string]string),
		DrainTimeout: 120 * time.Second, LastUpdated: e.Now(),
		engine:      e,
		processor:   host.New(e, host.DefaultConfig()),
		probeEvents: make(map[engine.EventID]struct{}),
	}
	s.transport = tcp.NewReceiver(e, tcp.ReceiverCallbacks{
		Send: s.emit,
		CanOpen: func() bool {
			halfOpenFull := s.MaxHalfOpen > 0 && s.HalfOpenConnections >= s.MaxHalfOpen
			return s.CanAcceptNewConnection() && !halfOpenFull
		},
		CanEstablish: func() bool {
			return s.MaxConnections == 0 || s.ActiveConnections < s.MaxConnections
		},
		OnConnectionCounts: func(active, halfOpen uint32) {
			s.ActiveConnections = active
			s.HalfOpenConnections = halfOpen
			s.LastUpdated = e.Now()
		},
		OnPayloadReceived: func(bytes int) {
			if s.OnPayloadReceived != nil {
				s.OnPayloadReceived(bytes)
			}
		},
		OnPeerFIN: s.recordConnectionLatency,
	})
	return s
}

func (s *Server) SetSender(sender func(model.Packet)) { s.sender = sender }

// Start finishes backend configuration; CPU work is scheduled only on arrival.
func (s *Server) Start() {
	if s.started || s.Killed {
		return
	}
	s.started = true
}

func (s *Server) Shutdown() {
	if s.Killed {
		return
	}
	s.Killed = true
	s.Active = false
	s.HealthTCP = false
	s.processor.Stop()
	for event := range s.probeEvents {
		s.engine.Cancel(event)
	}
	clear(s.probeEvents)
	s.transport.Shutdown()
	s.ActiveConnections = 0
	s.HalfOpenConnections = 0
	s.LastUpdated = s.engine.Now()
}

// MarkFailed represents a physical failure. A failed health probe should call
// UpdateHealth instead, so a reachable target can answer a later probe.
func (s *Server) MarkFailed() {
	if s.Killed {
		return
	}
	s.processor.DiscardPending()
	s.Active = false
	s.HealthTCP = false
	s.LatencyP95Milli = math.MaxUint32
	s.Draining = true
	s.DrainStartedAt = s.engine.Now()
	s.LastUpdated = s.engine.Now()
}

func (s *Server) MarkHealthy() {
	if s.Killed {
		return
	}
	s.Active = true
	s.HealthTCP = true
	s.LatencyP95Milli = 0
	s.Draining = false
	s.DrainStartedAt = 0
	s.LastUpdated = s.engine.Now()
}

func (s *Server) StartDrain() {
	if s.Killed {
		return
	}
	s.Draining = true
	s.DrainStartedAt = s.engine.Now()
	s.LastUpdated = s.engine.Now()
}

func (s *Server) StopDrain() {
	if s.Killed {
		return
	}
	s.Draining = false
	s.DrainStartedAt = 0
	s.LastUpdated = s.engine.Now()
}

// HandleProbe schedules an asynchronous reply. Observed health and draining
// do not make a physically reachable server unable to answer a probe.
func (s *Server) HandleProbe(onReply func()) {
	if s.Killed || !s.Active || onReply == nil {
		return
	}
	var event engine.EventID
	event = s.engine.ScheduleAfter(0, func() {
		delete(s.probeEvents, event)
		if !s.Killed && s.Active {
			onReply()
		}
	})
	if event != 0 {
		s.probeEvents[event] = struct{}{}
	}
}

func (s *Server) emit(pkt model.Packet) {
	if !s.Killed && s.Active && s.sender != nil {
		s.sender(pkt)
	}
}

// This preserves the old metric: the p95 of SYN-to-FIN connection lifetimes
// over the last 100 finished connections, not request latency or network RTT.
func (s *Server) recordConnectionLatency(elapsed time.Duration) {
	s.latencySamples[s.latencyPosition] = elapsed.Milliseconds()
	s.latencyPosition = (s.latencyPosition + 1) % len(s.latencySamples)
	if s.latencyCount < len(s.latencySamples) {
		s.latencyCount++
	}
	samples := append([]int64(nil), s.latencySamples[:s.latencyCount]...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := (95*s.latencyCount+99)/100 - 1
	s.LatencyP95Milli = uint32(min(samples[index], int64(math.MaxUint32)))
}

func (s *Server) CanAcceptNewConnection() bool {
	return s.IsHealthy() && (s.MaxConnections == 0 || s.ActiveConnections < s.MaxConnections)
}

func (s *Server) UpdateHealth(healthTCP bool, latencyP95Milli uint32) {
	if s.Killed {
		return
	}
	s.HealthTCP = healthTCP
	s.LatencyP95Milli = latencyP95Milli
	s.LastUpdated = s.engine.Now()
}

func (s *Server) ShouldBeTerminated() bool {
	return s.Draining && s.ActiveConnections == 0 && s.HalfOpenConnections == 0
}

func (s *Server) IsHealthy() bool {
	return !s.Killed && s.Active && s.HealthTCP && !s.Draining
}

func (s *Server) Score() float64 {
	if !s.IsHealthy() {
		return math.Inf(1)
	}
	return float64(s.LatencyP95Milli) / float64(max(1, int(s.Weight)))
}

func (s *Server) GetState() model.ServerState {
	processing := s.processor.Stats()
	return model.ServerState{
		ID: s.ID, Name: s.Name, Address: s.Address, Port: s.Port,
		Active: s.Active, ActiveConnections: s.ActiveConnections,
		HalfOpenConnections: s.HalfOpenConnections, LatencyP95Milli: s.LatencyP95Milli,
		CpuUsage:               processing.Utilization(),
		Host:                   processing,
		NumberOfHandledPackets: s.NumberOfHandledPackets,
		ReceivedPayloadBytes:   s.transport.ReceivedPayloadBytes(),
		ECN:                    s.transport.GetECNState(),
		ACK:                    s.transport.GetACKState(),
	}
}
