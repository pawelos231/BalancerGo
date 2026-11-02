package client

import (
	"time"

	"load-balancer/model"
)

// SendCompleteStream may be called before EstablishConnection. A client owns
// one finite stream; data starts only after the handshake has completed.
func (c *Client) SendCompleteStream(dataSize int, gap time.Duration) {
	if dataSize < 0 || gap < 0 {
		panic("client: stream size and gap must be nonnegative")
	}
	if c.terminated || c.connection.requested || c.streamRequested {
		return
	}
	c.streamRequested = true
	c.unsentPayloadBytes = dataSize
	c.remainingPayloadBytesToConfirm = dataSize
	c.gap = gap
	if c.handshakeCompleted {
		c.updateNextSendTime()
		c.queueSegment()
	}
}

// queueSegment schedules one new segment, then lets the engine run other events.
// A 4096-byte window can release four 1024-byte segments without waiting for ACKs.
func (c *Client) queueSegment() {
	canSend := c.handshakeCompleted && c.streamRequested
	if c.terminated || c.connection.finSent || !canSend {
		return
	}
	c.observeSender()
	if c.remainingPayloadBytesToConfirm == 0 {
		c.finishStream()
		return
	}
	if !c.nextNewSegmentWithinRun {
		return
	}
	if c.nextPayloadSize() == 0 {
		// All bytes have been sent, or the window has no room for another segment.
		return
	}

	// Pacing constrains new data only; zero preserves Reno/Tahoe's existing event order.
	policyDeadline := c.congestion.policy.State().NextSendAt
	nextSendAt := max(c.engine.Now(), c.nextNewSegmentAt, policyDeadline)
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
		if max(c.nextNewSegmentAt, policyDeadline) > c.engine.Now() {
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
		c.unsentPayloadBytes -= payloadBytes
		c.pendingSegments.append(packet)
		c.recordPayloadStart()
		c.updateNextSendTime()
		c.observeSender()
		c.startRTTProbe(packet)
		if c.dataTimer == 0 {
			c.armDataTimer()
		}
		c.congestion.recordDataSent(c.engine.Now(), packet.Size)

		// All sender state is ready before a synchronous reply can arrive.
		c.emit(packet)
		c.queueSegment()
	})
}

// updateNextSendTime starts the pause from readiness or an actual new send.
// With a 10 ms gap, a send at 30 ms makes the next one eligible at 40 ms.
func (c *Client) updateNextSendTime() {
	remainingTime := c.engine.EndTime() - c.engine.Now()
	// Check whether the gap leaves time to start sending within this run.
	// example: gap = 10ms, remainingTime (of simulation) 5ms, then we cant send it, beacuse it would be out of the scope of this simulation
	c.nextNewSegmentWithinRun = c.gap <= remainingTime
	if c.nextNewSegmentWithinRun {
		c.nextNewSegmentAt = c.engine.Now() + c.gap
	}
}

// nextPayloadSize fits new data inside both receiver and congestion windows.
// Already-sent bytes are excluded by nextSendSeq and must not be subtracted twice.
func (c *Client) nextPayloadSize() int {
	if c.peerWindowEnd <= c.nextSendSeq {
		return 0
	}

	availableBytes := c.peerWindowEnd - c.nextSendSeq
	availableBytes = min(availableBytes, c.availableCongestionWindowBytes())
	payloadLimit := min(c.unsentPayloadBytes, model.SEGMENT_SIZE)
	return int(min(uint64(payloadLimit), availableBytes))
}
