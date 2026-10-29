package server

import (
	"math"
	"sort"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

const statisticsInterval = 100 * time.Millisecond

// Server receives TCP byte ranges and replies with cumulative ACKs and a window.
// Closing still uses the simplified FIN exchange rather than a full TCP FSM.
// All access belongs to the engine's single execution thread.
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
	CpuUsage        uint32

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

	receivedPayloadBytes uint64
	receiveBufferBytes   uint16
	engine               *engine.Engine
	sender               func(model.Packet)
	connections          map[model.ClientKey]*connection
	started              bool
	statisticsEvent      engine.EventID
	previousPacketCount  uint32
	probeEvents          map[engine.EventID]struct{}
	latencySamples       [100]int64
	latencyCount         int
	latencyPosition      int
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
	return &Server{
		ID: id, Name: name, Address: address, Port: port,
		Distance: distance, Weight: weight, MaxHalfOpen: maxHalfOpen,
		Active: true, HealthTCP: true,
		Tags: []string{}, Metadata: make(map[string]string),
		DrainTimeout: 120 * time.Second, LastUpdated: e.Now(),
		engine: e, connections: make(map[model.ClientKey]*connection),
		probeEvents:        make(map[engine.EventID]struct{}),
		receiveBufferBytes: model.DefaultReceiveBufferBytes,
	}
}

func (s *Server) SetSender(sender func(model.Packet)) { s.sender = sender }

// SetReceiveBufferBytes sets each connection's capacity before any packet arrives.
func (s *Server) SetReceiveBufferBytes(capacity int) {
	if capacity < 1 || capacity > math.MaxUint16 {
		panic("server: receive buffer capacity must be in 1..65535")
	}
	if s.NumberOfHandledPackets != 0 || len(s.connections) != 0 {
		panic("server: receive buffer capacity must be set before traffic")
	}

	s.receiveBufferBytes = uint16(capacity)
}

// Start schedules the old periodic packet-count proxy, once per server.
func (s *Server) Start() {
	if s.started || s.Killed {
		return
	}
	s.started = true
	s.previousPacketCount = s.NumberOfHandledPackets
	s.scheduleStatistics()
}

func (s *Server) scheduleStatistics() {
	s.statisticsEvent = s.engine.ScheduleAfter(statisticsInterval, func() {
		s.statisticsEvent = 0
		if s.Killed {
			return
		}
		s.CpuUsage = s.NumberOfHandledPackets - s.previousPacketCount
		s.previousPacketCount = s.NumberOfHandledPackets
		s.scheduleStatistics()
	})
}

func (s *Server) Shutdown() {
	if s.Killed {
		return
	}
	s.Killed = true
	s.Active = false
	s.HealthTCP = false
	s.CpuUsage = 0
	s.engine.Cancel(s.statisticsEvent)
	s.statisticsEvent = 0
	for event := range s.probeEvents {
		s.engine.Cancel(event)
	}
	clear(s.probeEvents)
	clear(s.connections)
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
	return model.ServerState{
		ID: s.ID, Name: s.Name, Address: s.Address, Port: s.Port,
		Active: s.Active, ActiveConnections: s.ActiveConnections,
		HalfOpenConnections: s.HalfOpenConnections, LatencyP95Milli: s.LatencyP95Milli,
		CpuUsage:               s.CpuUsage,
		NumberOfHandledPackets: s.NumberOfHandledPackets,
		ReceivedPayloadBytes:   s.receivedPayloadBytes,
	}
}
