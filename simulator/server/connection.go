package server

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// connection owns one peer's handshake, receive buffer and closing timers.
type connection struct {
	startedAt      time.Duration
	established    bool
	state          model.TCPState
	peerInitialSeq uint64
	sendNextSeq    uint64
	receiver       receiveBuffer
	lastActivity   time.Duration
	idleEvent      engine.EventID

	// Keep the first SYN's SACK agreement and recently reported blocks.
	sackPermitted   bool
	sackRecent      [model.MaxSACKBlocks]model.SACKBlock
	sackRecentCount int

	closeStarted   bool
	closeStartedAt time.Duration
	outcome        model.CloseOutcome
	finSent        bool
	finAcked       bool
	finSeq         uint64
	finRetries     uint64
	finTimeouts    int
	finRTO         time.Duration
	finEvent       engine.EventID
	waitEvent      engine.EventID
	peerFIN        bool
	peerFINSeq     uint64
	pendingFIN     bool
	pendingFINSeq  uint64
}

func (s *Server) HandlePacket(pkt model.Packet) {
	if s.Killed || !s.Active || pkt.Key.Protocol != model.TCP {
		return
	}
	s.NumberOfHandledPackets++
	sequenceEnd, valid := pkt.SequenceEnd()
	if !valid || !pkt.ValidSACKOptions() ||
		pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagFIN != 0 {
		return
	}
	conn := s.connections[pkt.Key]
	if pkt.Flag&model.FlagRST != 0 {
		s.handleRST(pkt, conn)
		return
	}
	if pkt.Flag&model.FlagSYN != 0 {
		s.handleSYN(pkt, conn, sequenceEnd)
		return
	}
	if conn == nil || conn.state == model.TCPClosed || pkt.Flag&model.FlagACK == 0 ||
		!conn.validACK(pkt.Ack) {
		return
	}
	if pkt.Flag&model.FlagFIN != 0 {
		s.handleFIN(pkt, conn)
		return
	}

	// Data can complete the handshake when the standalone third ACK was lost.
	if !conn.established {
		validSequence := pkt.Seq == conn.receiver.nextExpectedSeq
		if pkt.Size > 0 {
			validSequence = conn.receiver.overlapsWindow(pkt.Seq, sequenceEnd)
		}
		if !validSequence || !s.establishConnection(pkt.Key, conn) {
			return
		}
	}
	if pkt.Size == 0 {
		if pkt.Seq == conn.receiver.nextExpectedSeq {
			if s.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
				s.recordConnectionActivity(pkt.Key, conn)
			}
		}
		return
	}
	if conn.peerFIN {
		return // The peer has already ended its byte stream.
	}
	if conn.receiver.overlapsWindow(pkt.Seq, sequenceEnd) {
		if !s.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
			return
		}
		s.recordConnectionActivity(pkt.Key, conn)
	}
	if conn.pendingFIN {
		sequenceEnd = min(sequenceEnd, conn.pendingFINSeq)
	}
	newBytes := conn.receiver.accept(pkt.Seq, sequenceEnd)
	conn.recordSACKArrival(pkt.Seq, sequenceEnd)
	if newBytes > 0 {
		s.receivedPayloadBytes += uint64(newBytes)
		if s.OnPayloadReceived != nil {
			s.OnPayloadReceived(newBytes)
		}
	}
	if s.connections[pkt.Key] != conn {
		return
	}
	if conn.pendingFIN && conn.receiver.nextExpectedSeq == conn.pendingFINSeq {
		s.consumeFIN(pkt.Key, conn, conn.pendingFINSeq)
		return
	}

	// Duplicates and data outside the window repeat the current missing position.
	s.acknowledge(pkt.Key, conn)
}

// validACK permits an old ACK while our FIN is still in flight.
func (conn *connection) validACK(ack uint64) bool {
	return ack >= 1 && ack <= conn.sendNextSeq
}

// acknowledge reports the first missing byte and the range we can still accept.
func (s *Server) acknowledge(key model.ClientKey, conn *connection) {
	s.emit(model.Packet{
		Key:                key,
		Flag:               model.FlagACK,
		Seq:                conn.sendNextSeq,
		Ack:                conn.receiver.nextExpectedSeq,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
		SACKBlocks:         conn.sackBlocks(),
	})
}

// handleSYN reserves one flow; repeated SYNs keep its original byte positions.
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
			startedAt:      s.engine.Now(),
			state:          model.TCPSynReceived,
			peerInitialSeq: pkt.Seq,
			sendNextSeq:    1, // Fixed backend ISN zero; SYN occupies one position.
			sackPermitted:  s.sackEnabled && pkt.SACKPermitted,
			receiver: receiveBuffer{
				nextExpectedSeq: sequenceEnd,
				capacity:        s.receiveBufferBytes,
			},
		}
		s.connections[pkt.Key] = conn
		s.HalfOpenConnections++
		s.LastUpdated = s.engine.Now()

		// Retransmitted SYNs keep this first handshake deadline.
		conn.lastActivity = s.engine.Now()
		s.scheduleIdleTimeout(pkt.Key, conn, s.halfOpenIdleTimeout)
	}
	if pkt.Seq != conn.peerInitialSeq || conn.finSent || conn.state == model.TCPClosed {
		return
	}
	s.emit(model.Packet{
		Key:                pkt.Key,
		Flag:               model.FlagSYN | model.FlagACK,
		Seq:                0,
		Ack:                conn.peerInitialSeq + 1,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
		SACKPermitted:      conn.sackPermitted,
	})
}

// establishConnection moves one reservation from half-open to active once.
func (s *Server) establishConnection(key model.ClientKey, conn *connection) bool {
	if s.MaxConnections > 0 && s.ActiveConnections >= s.MaxConnections {
		s.ForgetConnection(key)
		return false
	}
	conn.established = true
	conn.state = model.TCPEstablished
	s.cancelIdleTimer(conn)
	s.recordConnectionActivity(key, conn)
	if s.HalfOpenConnections > 0 {
		s.HalfOpenConnections--
	}
	s.ActiveConnections++
	s.LastUpdated = s.engine.Now()
	return true
}

// ForgetConnection releases local state and cancels every connection timer.
func (s *Server) ForgetConnection(key model.ClientKey) {
	if conn := s.connections[key]; conn != nil {
		s.finishConnection(key, conn, model.CloseAborted)
	}
}
