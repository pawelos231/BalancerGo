package receiver

import "load-balancer/model"

// SetCloseConfig chooses the virtual-time limits before traffic starts.
func (r *Receiver) SetCloseConfig(config model.CloseConfig) {
	if !config.Valid() {
		panic("tcp: invalid close configuration")
	}
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: close configuration must be set before traffic")
	}
	r.closeConfig = config
}

// SetRTOConfig uses the configured initial timeout for FIN, without RTT sampling.
func (r *Receiver) SetRTOConfig(config model.RTOConfig) {
	validMode := config.Mode == model.RTOFixed || config.Mode == model.RTOAdaptive
	validLimits := config.MinimumTimeout > 0 && config.MinimumTimeout <= config.InitialTimeout &&
		config.InitialTimeout <= config.MaximumTimeout
	if !validMode || !validLimits {
		panic("tcp: invalid RTO configuration")
	}
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: RTO configuration must be set before traffic")
	}
	r.rtoConfig = config
}

func (r *Receiver) GetConnectionState(key model.ClientKey) model.CloseState {
	conn := r.connections[key]
	if conn == nil {
		return model.CloseState{State: model.TCPClosed}
	}
	state := model.CloseState{
		State:              conn.state,
		Outcome:            conn.outcome,
		FINRetransmissions: conn.finRetries,
	}
	if conn.closeStarted {
		state.Elapsed = r.engine.Now() - conn.closeStartedAt
	}
	return state
}

// CloseConnection ends our sending direction; the peer may still send payload.
func (r *Receiver) CloseConnection(key model.ClientKey) {
	conn := r.connections[key]
	if conn == nil || !conn.established || conn.state != model.TCPEstablished {
		return
	}
	r.startFIN(key, conn, model.TCPFinWait1)
}

// AbortConnection sends a reset from the current sequence positions.
func (r *Receiver) AbortConnection(key model.ClientKey) {
	conn := r.connections[key]
	if conn == nil || conn.state == model.TCPClosed {
		return
	}
	packet := model.Packet{
		Key:  key,
		Flag: model.FlagRST | model.FlagACK,
		Seq:  conn.sendNextSeq,
		Ack:  conn.receiver.nextExpectedSeq,
	}
	r.cancelCloseTimers(conn)
	r.cancelDelayedACK(conn)
	conn.state = model.TCPClosed
	conn.outcome = model.CloseAborted
	r.emit(packet)
	r.finishConnection(key, conn, model.CloseAborted)
}

func (r *Receiver) handleRST(pkt model.Packet, conn *receiveConnection) {
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
	r.finishConnection(pkt.Key, conn, model.CloseReset)
}

// handleFIN accepts pure FINs only; payload with FIN is outside this profile.
func (r *Receiver) handleFIN(pkt model.Packet, conn *receiveConnection) {
	if pkt.Size != 0 || pkt.Flag&model.FlagPSH != 0 {
		return
	}
	if conn.peerFIN {
		if pkt.Seq != conn.peerFINSeq || !r.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
			return
		}
		r.observeECN(conn, pkt)
		if conn.state == model.TCPTimeWait {
			r.enterTimeWait(pkt.Key, conn)
		}
		if conn.state == model.TCPLastAck {
			conn.finRetries++
			r.emitFIN(pkt.Key, conn)
		} else {
			r.acknowledge(pkt.Key, conn)
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
	if !conn.established && !r.establishConnection(pkt.Key, conn) {
		return
	}
	if !r.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
		return
	}
	r.observeECN(conn, pkt)
	r.recordConnectionActivity(pkt.Key, conn)
	if pkt.Seq != conn.receiver.nextExpectedSeq {
		conn.pendingFIN = true
		conn.pendingFINSeq = pkt.Seq
		r.acknowledge(pkt.Key, conn)
		return
	}
	r.consumeFIN(pkt.Key, conn, pkt.Seq)
}

// consumeFIN runs only after every preceding payload byte has arrived.
func (r *Receiver) consumeFIN(key model.ClientKey, conn *receiveConnection, sequence uint64) {
	conn.pendingFIN = false
	conn.peerFIN = true
	conn.peerFINSeq = sequence
	conn.receiver.nextExpectedSeq++
	if !conn.closeStarted {
		conn.closeStarted = true
		conn.closeStartedAt = r.engine.Now()
	}
	r.recordPeerFIN(r.engine.Now() - conn.startedAt)
	if r.stopped || r.connections[key] != conn {
		return
	}
	switch conn.state {
	case model.TCPEstablished:
		conn.state = model.TCPCloseWait
		r.startFIN(key, conn, model.TCPLastAck)
		return
	case model.TCPFinWait1:
		conn.state = model.TCPClosing
	case model.TCPFinWait2:
		r.enterTimeWait(key, conn)
	}
	r.acknowledge(key, conn)
}

// acknowledgeFIN handles our FIN independently from the peer's FIN.
// It returns false when the ACK closes and removes the connection.
func (r *Receiver) acknowledgeFIN(key model.ClientKey, conn *receiveConnection, ack uint64) (stillOpen bool) {
	if !conn.finSent || conn.finAcked || ack != conn.sendNextSeq {
		return true
	}
	conn.finAcked = true
	r.engine.Cancel(conn.finEvent)
	conn.finEvent = 0
	switch conn.state {
	case model.TCPFinWait1:
		conn.state = model.TCPFinWait2
		conn.waitEvent = r.engine.ScheduleAfter(r.closeConfig.FinWaitTimeout, func() {
			conn.waitEvent = 0
			r.finishConnection(key, conn, model.CloseWaitTimeout)
		})
	case model.TCPClosing:
		r.enterTimeWait(key, conn)
	case model.TCPLastAck:
		r.finishConnection(key, conn, model.CloseGraceful)
		return false
	}
	return true
}

func (r *Receiver) startFIN(key model.ClientKey, conn *receiveConnection, state model.TCPState) {
	// Closing has its own retry and wait deadlines.
	r.cancelIdleTimer(conn)

	if !conn.closeStarted {
		conn.closeStarted = true
		conn.closeStartedAt = r.engine.Now()
	}
	conn.state = state
	conn.finSent = true
	conn.finSeq = conn.sendNextSeq
	conn.sendNextSeq++
	conn.finRTO = r.rtoConfig.InitialTimeout
	r.scheduleFIN(key, conn)
	r.emitFIN(key, conn)
}

func (r *Receiver) emitFIN(key model.ClientKey, conn *receiveConnection) {
	// FIN/ACK already confirms received data, so its old delayed ACK is unnecessary.
	r.cancelDelayedACK(conn)
	r.emit(model.Packet{
		Key:                key,
		Flag:               r.ackFlags(conn, model.FlagFIN|model.FlagACK),
		Seq:                conn.finSeq,
		Ack:                conn.receiver.nextExpectedSeq,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
		SACKBlocks:         conn.sackBlocks(),
	})
}

// scheduleFIN arms the next retry before emitting, so direct callbacks are safe.
func (r *Receiver) scheduleFIN(key model.ClientKey, conn *receiveConnection) {
	conn.finEvent = r.engine.ScheduleAfter(conn.finRTO, func() {
		conn.finEvent = 0
		if r.connections[key] != conn || conn.finAcked {
			return
		}
		if conn.finTimeouts >= r.closeConfig.MaxFINRetries {
			r.finishConnection(key, conn, model.CloseFINRetryLimit)
			return
		}
		conn.finTimeouts++
		conn.finRetries++
		if r.rtoConfig.ExponentialBackoff {
			maximum := r.rtoConfig.MaximumTimeout
			if conn.finRTO >= maximum-conn.finRTO {
				conn.finRTO = maximum
			} else {
				conn.finRTO *= 2
			}
		}
		r.scheduleFIN(key, conn)
		r.emitFIN(key, conn)
	})
}

func (r *Receiver) enterTimeWait(key model.ClientKey, conn *receiveConnection) {
	r.cancelCloseTimers(conn)
	r.cancelDelayedACK(conn)
	conn.state = model.TCPTimeWait
	conn.waitEvent = r.engine.ScheduleAfter(2*r.closeConfig.MSL, func() {
		conn.waitEvent = 0
		r.finishConnection(key, conn, model.CloseGraceful)
	})
}

func (r *Receiver) cancelCloseTimers(conn *receiveConnection) {
	r.engine.Cancel(conn.finEvent)
	r.engine.Cancel(conn.waitEvent)
	conn.finEvent = 0
	conn.waitEvent = 0
}

func (r *Receiver) finishConnection(key model.ClientKey, conn *receiveConnection, outcome model.CloseOutcome) {
	if r.connections[key] != conn {
		return
	}
	r.cancelIdleTimer(conn)
	r.cancelCloseTimers(conn)
	r.cancelDelayedACK(conn)
	conn.state = model.TCPClosed
	conn.outcome = outcome
	delete(r.connections, key)
	if conn.established {
		if r.activeConnections > 0 {
			r.activeConnections--
		}
	} else if r.halfOpenConnections > 0 {
		r.halfOpenConnections--
	}
	r.notifyConnectionCounts()
}
