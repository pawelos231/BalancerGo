package server

import "load-balancer/model"

// SetCloseConfig chooses the virtual-time limits before traffic starts.
func (s *Server) SetCloseConfig(config model.CloseConfig) {
	if !config.Valid() {
		panic("server: invalid close configuration")
	}
	if s.NumberOfHandledPackets != 0 || len(s.connections) != 0 {
		panic("server: close configuration must be set before traffic")
	}
	s.closeConfig = config
}

// SetRTOConfig uses the configured initial timeout for FIN, without RTT sampling.
func (s *Server) SetRTOConfig(config model.RTOConfig) {
	validMode := config.Mode == model.RTOFixed || config.Mode == model.RTOAdaptive
	validLimits := config.MinimumTimeout > 0 && config.MinimumTimeout <= config.InitialTimeout &&
		config.InitialTimeout <= config.MaximumTimeout
	if !validMode || !validLimits {
		panic("server: invalid RTO configuration")
	}
	if s.NumberOfHandledPackets != 0 || len(s.connections) != 0 {
		panic("server: RTO configuration must be set before traffic")
	}
	s.rtoConfig = config
}

func (s *Server) GetConnectionState(key model.ClientKey) model.CloseState {
	conn := s.connections[key]
	if conn == nil {
		return model.CloseState{State: model.TCPClosed}
	}
	state := model.CloseState{
		State:              conn.state,
		Outcome:            conn.outcome,
		FINRetransmissions: conn.finRetries,
	}
	if conn.closeStarted {
		state.Elapsed = s.engine.Now() - conn.closeStartedAt
	}
	return state
}

// CloseConnection ends our sending direction; the peer may still send payload.
func (s *Server) CloseConnection(key model.ClientKey) {
	conn := s.connections[key]
	if conn == nil || !conn.established || conn.state != model.TCPEstablished {
		return
	}
	s.startFIN(key, conn, model.TCPFinWait1)
}

// AbortConnection sends a reset from the current sequence positions.
func (s *Server) AbortConnection(key model.ClientKey) {
	conn := s.connections[key]
	if conn == nil || conn.state == model.TCPClosed {
		return
	}
	packet := model.Packet{
		Key:  key,
		Flag: model.FlagRST | model.FlagACK,
		Seq:  conn.sendNextSeq,
		Ack:  conn.receiver.nextExpectedSeq,
	}
	s.cancelCloseTimers(conn)
	conn.state = model.TCPClosed
	conn.outcome = model.CloseAborted
	s.emit(packet)
	s.finishConnection(key, conn, model.CloseAborted)
}

func (s *Server) handleRST(pkt model.Packet, conn *connection) {
	if conn == nil || conn.state == model.TCPClosed || pkt.Size != 0 ||
		pkt.Flag&(model.FlagSYN|model.FlagFIN|model.FlagPSH) != 0 ||
		pkt.Seq != conn.receiver.nextExpectedSeq {
		return
	}
	if pkt.Flag&model.FlagACK != 0 && !conn.validACK(pkt.Ack) {
		return
	}
	if conn.state == model.TCPSynReceived &&
		(pkt.Flag&model.FlagACK == 0 || pkt.Ack != conn.sendNextSeq) {
		return
	}
	s.finishConnection(pkt.Key, conn, model.CloseReset)
}

// handleFIN accepts pure FINs only; payload with FIN is outside this profile.
func (s *Server) handleFIN(pkt model.Packet, conn *connection) {
	if pkt.Size != 0 || pkt.Flag&model.FlagPSH != 0 {
		return
	}
	if conn.peerFIN {
		if pkt.Seq != conn.peerFINSeq || !s.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
			return
		}
		if conn.state == model.TCPTimeWait {
			s.enterTimeWait(pkt.Key, conn)
		}
		if conn.state == model.TCPLastAck {
			conn.finRetries++
			s.emitFIN(pkt.Key, conn)
		} else {
			s.acknowledge(pkt.Key, conn)
		}
		return
	}
	if pkt.Seq < conn.receiver.nextExpectedSeq ||
		pkt.Seq-conn.receiver.nextExpectedSeq >= uint64(conn.receiver.windowBytes()) {
		return
	}
	if conn.pendingFIN && pkt.Seq != conn.pendingFINSeq {
		return
	}
	for _, buffered := range conn.receiver.ranges {
		if buffered.end > pkt.Seq {
			return // FIN cannot precede payload already accepted from this stream.
		}
	}
	if !conn.established && !s.establishConnection(pkt.Key, conn) {
		return
	}
	if !s.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
		return
	}
	if pkt.Seq != conn.receiver.nextExpectedSeq {
		conn.pendingFIN = true
		conn.pendingFINSeq = pkt.Seq
		s.acknowledge(pkt.Key, conn)
		return
	}
	s.consumeFIN(pkt.Key, conn, pkt.Seq)
}

// consumeFIN runs only after every preceding payload byte has arrived.
func (s *Server) consumeFIN(key model.ClientKey, conn *connection, sequence uint64) {
	conn.pendingFIN = false
	conn.peerFIN = true
	conn.peerFINSeq = sequence
	conn.receiver.nextExpectedSeq++
	if !conn.closeStarted {
		conn.closeStarted = true
		conn.closeStartedAt = s.engine.Now()
	}
	s.recordConnectionLatency(s.engine.Now() - conn.startedAt)
	switch conn.state {
	case model.TCPEstablished:
		conn.state = model.TCPCloseWait
		s.startFIN(key, conn, model.TCPLastAck)
		return
	case model.TCPFinWait1:
		conn.state = model.TCPClosing
	case model.TCPFinWait2:
		s.enterTimeWait(key, conn)
	}
	s.acknowledge(key, conn)
}

// acknowledgeFIN handles our FIN independently from the peer's FIN.
// It returns false when the ACK closes and removes the connection.
func (s *Server) acknowledgeFIN(key model.ClientKey, conn *connection, ack uint64) (stillOpen bool) {
	if !conn.finSent || conn.finAcked || ack != conn.sendNextSeq {
		return true
	}
	conn.finAcked = true
	s.engine.Cancel(conn.finEvent)
	conn.finEvent = 0
	switch conn.state {
	case model.TCPFinWait1:
		conn.state = model.TCPFinWait2
		conn.waitEvent = s.engine.ScheduleAfter(s.closeConfig.FinWaitTimeout, func() {
			conn.waitEvent = 0
			s.finishConnection(key, conn, model.CloseWaitTimeout)
		})
	case model.TCPClosing:
		s.enterTimeWait(key, conn)
	case model.TCPLastAck:
		s.finishConnection(key, conn, model.CloseGraceful)
		return false
	}
	return true
}

func (s *Server) startFIN(key model.ClientKey, conn *connection, state model.TCPState) {
	if !conn.closeStarted {
		conn.closeStarted = true
		conn.closeStartedAt = s.engine.Now()
	}
	conn.state = state
	conn.finSent = true
	conn.finSeq = conn.sendNextSeq
	conn.sendNextSeq++
	conn.finRTO = s.rtoConfig.InitialTimeout
	s.scheduleFIN(key, conn)
	s.emitFIN(key, conn)
}

func (s *Server) emitFIN(key model.ClientKey, conn *connection) {
	s.emit(model.Packet{
		Key:                key,
		Flag:               model.FlagFIN | model.FlagACK,
		Seq:                conn.finSeq,
		Ack:                conn.receiver.nextExpectedSeq,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
	})
}

// scheduleFIN arms the next retry before emitting, so direct callbacks are safe.
func (s *Server) scheduleFIN(key model.ClientKey, conn *connection) {
	conn.finEvent = s.engine.ScheduleAfter(conn.finRTO, func() {
		conn.finEvent = 0
		if s.connections[key] != conn || conn.finAcked {
			return
		}
		if conn.finTimeouts >= s.closeConfig.MaxFINRetries {
			s.finishConnection(key, conn, model.CloseFINRetryLimit)
			return
		}
		conn.finTimeouts++
		conn.finRetries++
		if s.rtoConfig.ExponentialBackoff {
			maximum := s.rtoConfig.MaximumTimeout
			if conn.finRTO >= maximum-conn.finRTO {
				conn.finRTO = maximum
			} else {
				conn.finRTO *= 2
			}
		}
		s.scheduleFIN(key, conn)
		s.emitFIN(key, conn)
	})
}

func (s *Server) enterTimeWait(key model.ClientKey, conn *connection) {
	s.cancelCloseTimers(conn)
	conn.state = model.TCPTimeWait
	conn.waitEvent = s.engine.ScheduleAfter(2*s.closeConfig.MSL, func() {
		conn.waitEvent = 0
		s.finishConnection(key, conn, model.CloseGraceful)
	})
}

func (s *Server) cancelCloseTimers(conn *connection) {
	s.engine.Cancel(conn.finEvent)
	s.engine.Cancel(conn.waitEvent)
	conn.finEvent = 0
	conn.waitEvent = 0
}

func (s *Server) finishConnection(key model.ClientKey, conn *connection, outcome model.CloseOutcome) {
	if s.connections[key] != conn {
		return
	}
	s.cancelCloseTimers(conn)
	conn.state = model.TCPClosed
	conn.outcome = outcome
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
