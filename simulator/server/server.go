package server

import (
	"math"
	"sort"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

const statisticsInterval = 100 * time.Millisecond

type connection struct {
	startedAt       time.Duration
	established     bool
	peerInitialSeq  uint64
	nextExpectedSeq uint64
	sendNextSeq     uint64
}

// Server retains the simplified SYN/ACK, PSH/ACK and FIN/ACK exchanges. The
// connection map only makes their accounting idempotent; it is not a TCP FSM.
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
		probeEvents: make(map[engine.EventID]struct{}),
	}
}

func (s *Server) SetSender(sender func(model.Packet)) { s.sender = sender }

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

func (s *Server) HandlePacket(pkt model.Packet) {
	if s.Killed || !s.Active || pkt.Key.Protocol != model.TCP {
		return
	}
	s.NumberOfHandledPackets++
	sequenceEnd, valid := pkt.SequenceEnd()
	invalidControls := pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagFIN != 0
	if !valid || invalidControls || pkt.Flag&model.FlagRST != 0 {
		return
	}
	conn, known := s.connections[pkt.Key]

	if pkt.Flag&model.FlagSYN != 0 {
		s.handleSYN(pkt, conn, sequenceEnd)
		return
	}

	// The backend sends no payload in this profile, so ACK must cover its SYN.
	if !known || pkt.Flag&model.FlagACK == 0 || pkt.Ack != conn.sendNextSeq {
		return
	}
	if pkt.Flag&model.FlagFIN != 0 {
		s.handleFIN(pkt, conn, sequenceEnd)
		return
	}

	// A first data packet can finish the handshake if the standalone ACK was lost.
	if !conn.established {
		if pkt.Seq != conn.nextExpectedSeq || !s.establishConnection(pkt.Key, conn) {
			return
		}
	}
	if pkt.Size == 0 {
		return // Pure ACK consumes no sequence space and needs no reply.
	}

	// Out-of-order buffering and partial-overlap handling belong to the next stage.
	futureData := pkt.Seq > conn.nextExpectedSeq
	partialOverlap := pkt.Seq < conn.nextExpectedSeq && sequenceEnd > conn.nextExpectedSeq
	if futureData || partialOverlap {
		return
	}
	if pkt.Seq == conn.nextExpectedSeq {
		conn.nextExpectedSeq = sequenceEnd
		s.receivedPayloadBytes += uint64(pkt.Size)
		if s.OnPayloadReceived != nil {
			s.OnPayloadReceived(pkt.Size)
		}
	}

	// Repeated complete ranges get the current ACK without counting bytes again.
	s.emit(model.Packet{
		Key:  pkt.Key,
		Flag: model.FlagACK,
		Seq:  conn.sendNextSeq,
		Ack:  conn.nextExpectedSeq,
	})
}

// handleSYN reserves one flow and repeats the same SYN-ACK for retransmissions.
func (s *Server) handleSYN(pkt model.Packet, conn *connection, sequenceEnd uint64) {
	if pkt.Size != 0 || pkt.Flag&model.FlagACK != 0 {
		return
	}
	if conn == nil {
		halfOpenFull := s.MaxHalfOpen > 0 && s.HalfOpenConnections >= s.MaxHalfOpen
		if !s.CanAcceptNewConnection() || halfOpenFull {
			return
		}
		conn = &connection{
			startedAt:       s.engine.Now(),
			peerInitialSeq:  pkt.Seq,
			nextExpectedSeq: sequenceEnd,
			sendNextSeq:     1, // Fixed backend ISN zero; SYN occupies one position.
		}
		s.connections[pkt.Key] = conn
		s.HalfOpenConnections++
		s.LastUpdated = s.engine.Now()
	}
	if pkt.Seq != conn.peerInitialSeq {
		return // Another SYN cannot reset an existing flow's byte positions.
	}
	s.emit(model.Packet{
		Key:  pkt.Key,
		Flag: model.FlagSYN | model.FlagACK,
		Seq:  0,
		Ack:  conn.peerInitialSeq + 1,
	})
}

// establishConnection moves one reservation from half-open to active once.
func (s *Server) establishConnection(key model.ClientKey, conn *connection) bool {
	if s.MaxConnections > 0 && s.ActiveConnections >= s.MaxConnections {
		s.ForgetConnection(key)
		return false
	}
	conn.established = true
	if s.HalfOpenConnections > 0 {
		s.HalfOpenConnections--
	}
	s.ActiveConnections++
	s.LastUpdated = s.engine.Now()
	return true
}

// handleFIN preserves immediate cleanup while validating the closing byte position.
// Full closing states and FIN retransmission remain a later transport stage.
func (s *Server) handleFIN(pkt model.Packet, conn *connection, sequenceEnd uint64) {
	if pkt.Size != 0 || pkt.Seq != conn.nextExpectedSeq {
		return
	}
	if conn.established {
		s.recordConnectionLatency(s.engine.Now() - conn.startedAt)
	}
	s.ForgetConnection(pkt.Key)
	s.emit(model.Packet{
		Key:  pkt.Key,
		Flag: model.FlagFIN | model.FlagACK,
		Seq:  conn.sendNextSeq,
		Ack:  sequenceEnd,
	})
}

// ForgetConnection is also used by LB timeout/closure cleanup. Repeated calls
// are harmless and never decrement counters belonging to another connection.
func (s *Server) ForgetConnection(key model.ClientKey) {
	conn, ok := s.connections[key]
	if !ok {
		return
	}
	delete(s.connections, key)
	if conn.established {
		if s.ActiveConnections > 0 {
			s.ActiveConnections--
		}
	} else if s.HalfOpenConnections > 0 {
		s.HalfOpenConnections--
	}
	s.LastUpdated = s.engine.Now()
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
