package receiver

import "load-balancer/model"

// SetDelayedACKConfig chooses the receiver's ACK policy before traffic starts.
func (r *Receiver) SetDelayedACKConfig(config model.DelayedACKConfig) {
	if !config.Valid() {
		panic("tcp: delayed ACK requires 1..2 segments and a delay in (0, 500ms]")
	}
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: delayed ACK must be configured before traffic")
	}
	r.delayedACKConfig = config
	r.ackState.Enabled = config.Enabled
}

func (r *Receiver) GetACKState() model.ACKState { return r.ackState }

// acknowledgeData waits only for fresh, contiguous data while the connection is open.
// Example: segments at 10 ms and 18 ms share an ACK at 18 ms; alone, the first waits 40 ms.
func (r *Receiver) acknowledgeData(key model.ClientKey, conn *receiveConnection, mayDelay bool) {
	config := r.delayedACKConfig
	if !config.Enabled || !mayDelay || conn.state != model.TCPEstablished || conn.pendingFIN {
		r.acknowledge(key, conn)
		return
	}

	conn.pendingACKSegmentCount++
	if conn.pendingACKSegmentCount >= config.SegmentsPerACK {
		r.acknowledge(key, conn)
		return
	}

	// Keep the first arrival's deadline; later arrivals cannot postpone it.
	if conn.pendingACKSegmentCount == 1 {
		conn.delayedACKEvent = r.engine.ScheduleAfter(config.MaxDelay, func() {
			if r.stopped || r.connections[key] != conn || conn.pendingACKSegmentCount == 0 {
				return
			}
			conn.delayedACKEvent = 0
			r.ackState.TimerTriggeredACKCount++
			r.acknowledge(key, conn)
		})
	}
}

// acknowledge reports current ACK/rwnd/SACK/ECE, including any data received while waiting.
func (r *Receiver) acknowledge(key model.ClientKey, conn *receiveConnection) {
	if r.stopped || r.connections[key] != conn {
		return
	}
	// Reset before sending: the callback can immediately deliver the next segment.
	r.cancelDelayedACK(conn)
	r.ackState.SentACKCount++
	r.emit(model.Packet{
		Key:                key,
		Flag:               r.ackFlags(conn, model.FlagACK),
		Seq:                conn.sendNextSeq,
		Ack:                conn.receiver.nextExpectedSeq,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
		SACKBlocks:         conn.sackBlocks(),
	})
}

// cancelDelayedACK also clears the count when a FIN/ACK confirms pending data.
func (r *Receiver) cancelDelayedACK(conn *receiveConnection) {
	r.engine.Cancel(conn.delayedACKEvent)
	conn.delayedACKEvent = 0
	conn.pendingACKSegmentCount = 0
}
