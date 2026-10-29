package server

import (
	"time"

	"load-balancer/model"
)

// connection owns one peer's handshake state and receive buffer.
type connection struct {
	startedAt      time.Duration
	established    bool
	peerInitialSeq uint64
	sendNextSeq    uint64
	receiver       receiveBuffer
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
	conn := s.connections[pkt.Key]

	if pkt.Flag&model.FlagSYN != 0 {
		s.handleSYN(pkt, conn, sequenceEnd)
		return
	}

	// The backend sends no payload, so the peer's ACK must cover its SYN.
	if conn == nil || pkt.Flag&model.FlagACK == 0 || pkt.Ack != conn.sendNextSeq {
		return
	}
	if pkt.Flag&model.FlagFIN != 0 {
		s.handleFIN(pkt, conn, sequenceEnd)
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
		return // A pure ACK needs no reply.
	}

	newBytes := conn.receiver.accept(pkt.Seq, sequenceEnd)
	if newBytes > 0 {
		s.receivedPayloadBytes += uint64(newBytes)
		if s.OnPayloadReceived != nil {
			s.OnPayloadReceived(newBytes)
		}
	}

	// Duplicates and data outside the window repeat the current missing position.
	s.acknowledge(pkt.Key, conn)
}

// acknowledge reports the first missing byte and the range we can still accept.
func (s *Server) acknowledge(key model.ClientKey, conn *connection) {
	s.emit(model.Packet{
		Key:                key,
		Flag:               model.FlagACK,
		Seq:                conn.sendNextSeq,
		Ack:                conn.receiver.nextExpectedSeq,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
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
			peerInitialSeq: pkt.Seq,
			sendNextSeq:    1, // Fixed backend ISN zero; SYN occupies one position.
			receiver: receiveBuffer{
				nextExpectedSeq: sequenceEnd,
				capacity:        s.receiveBufferBytes,
			},
		}
		s.connections[pkt.Key] = conn
		s.HalfOpenConnections++
		s.LastUpdated = s.engine.Now()
	}
	if pkt.Seq != conn.peerInitialSeq {
		return // Another SYN cannot reset an existing connection.
	}
	s.emit(model.Packet{
		Key:                pkt.Key,
		Flag:               model.FlagSYN | model.FlagACK,
		Seq:                0,
		Ack:                conn.peerInitialSeq + 1,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
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

// handleFIN preserves immediate cleanup after validating the closing position.
func (s *Server) handleFIN(pkt model.Packet, conn *connection, sequenceEnd uint64) {
	if pkt.Size != 0 || pkt.Seq != conn.receiver.nextExpectedSeq {
		return
	}
	if conn.established {
		s.recordConnectionLatency(s.engine.Now() - conn.startedAt)
	}
	conn.receiver.nextExpectedSeq = sequenceEnd
	s.ForgetConnection(pkt.Key)
	s.emit(model.Packet{
		Key:                pkt.Key,
		Flag:               model.FlagFIN | model.FlagACK,
		Seq:                conn.sendNextSeq,
		Ack:                sequenceEnd,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
	})
}

// ForgetConnection releases local state; repeated calls cannot decrement it twice.
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
