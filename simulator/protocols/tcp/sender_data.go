package tcp

import "load-balancer/model"

// SetSource accepts one finite source, before or after handshake.
// It reads workload timing without creating a separate generation event for each byte range.
func (c *Sender) SetSource(source PayloadSource) bool {
	if source == nil || source.RemainingBytes() < 0 {
		panic("tcp: payload source must contain a nonnegative byte count")
	}
	if c.terminated || c.connection.requested || c.source != nil {
		return false
	}
	c.source = source
	c.remainingPayloadBytesToConfirm = source.RemainingBytes()
	if c.handshakeCompleted {
		c.source.Start(c.engine.Now())
		c.queueSegment()
	}
	return true
}

// queueSegment schedules one new segment, then lets the engine run other events.
// A 4096-byte window can release four 1024-byte segments without waiting for ACKs.
func (c *Sender) queueSegment() {
	if c.sack.active {
		c.sendSACKRepairs()
	}
	canSend := c.handshakeCompleted && c.source != nil
	if c.terminated || c.connection.finSent || !canSend {
		return
	}
	c.observeSender()
	if c.remainingPayloadBytesToConfirm == 0 {
		c.finishStream()
		return
	}
	if c.ecn.holdBeyondRun {
		return
	}
	nextPayloadAt, withinRun := c.source.NextSendAt()
	if !withinRun {
		return
	}
	if c.nextPayloadSize() == 0 {
		// All bytes have been sent, or the window has no room for another segment.
		return
	}

	// Pacing constrains new data only; zero preserves Reno/Tahoe's existing event order.
	policyDeadline := c.congestion.policy.State().NextSendAt
	nextSendAt := max(c.engine.Now(), nextPayloadAt, policyDeadline, c.ecn.holdUntil)
	if c.sendEvent != 0 {
		// Keep an existing slot unless an ACK now permits an earlier paced send.
		if nextSendAt >= c.sendEventAt {
			return
		}
		c.cancelEvent(&c.sendEvent)
	}
	c.sendEventAt = nextSendAt
	c.sendEvent = c.engine.ScheduleAt(nextSendAt, func() {
		c.sendEvent = 0
		if c.terminated || c.connection.finSent {
			return
		}

		// An ACK may move the policy deadline after this callback was scheduled.
		// Requeue instead of sending early, without restarting the workload's gap.
		policyDeadline := c.congestion.policy.State().NextSendAt
		nextPayloadAt, _ := c.source.NextSendAt()
		if max(nextPayloadAt, policyDeadline, c.ecn.holdUntil) > c.engine.Now() {
			c.queueSegment()
			return
		}

		c.congestion.restartAfterIdle(c.engine.Now(), c.rto.state.CurrentRTO)
		c.observeSender()

		// Recheck credit: an ACK may have changed the window during the gap.
		payloadBytes := c.nextPayloadSize()
		if payloadBytes == 0 {
			return
		}

		// Data also acknowledges the handshake if its final standalone ACK was lost.
		packet := GeneratePacket(
			c.key,
			model.FlagPSH|model.FlagACK,
			payloadBytes,
			c.nextSendSeq,
			c.nextReceiveSeq,
		)
		c.nextSendSeq += uint64(payloadBytes)
		c.pendingSegments.append(packet)
		if c.sack.active {
			c.sack.pipeBytes += uint64(payloadBytes)
		}
		c.recordPayloadStart()
		c.source.Sent(payloadBytes, c.engine.Now())
		c.observeSender()
		c.startRTTProbe(packet)
		if c.dataTimer == 0 {
			c.armDataTimer()
		}
		c.congestion.recordDataSent(c.engine.Now(), packet.Size)
		c.decorateNewPayload(&packet)

		// All sender state is ready before a synchronous reply can arrive.
		c.emit(packet)
		c.queueSegment()
	})
}

// unsentBytes reads workload demand; TCP separately retains already-sent pending data.
func (c *Sender) unsentBytes() int {
	if c.source == nil {
		return 0
	}
	return c.source.RemainingBytes()
}

// nextPayloadSize fits new data inside both receiver and congestion windows.
// Already-sent bytes are excluded by nextSendSeq and must not be subtracted twice.
func (c *Sender) nextPayloadSize() int {
	if c.peerWindowEnd <= c.nextSendSeq {
		return 0
	}

	availableBytes := c.peerWindowEnd - c.nextSendSeq
	congestionSpace := c.availableCongestionWindowBytes()
	// RFC 6675 recovery waits for one MSS of cwnd space before choosing another send.
	if c.sack.active && congestionSpace < uint64(c.mssBytes) {
		return 0
	}
	availableBytes = min(availableBytes, congestionSpace)
	payloadLimit := min(c.unsentBytes(), c.mssBytes)
	return int(min(uint64(payloadLimit), availableBytes))
}
