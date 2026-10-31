package client

import (
	"time"

	"load-balancer/model"
)

// roundTripProbe measures one original segment; retransmission makes it ambiguous.
// recoveryUntilSeq prevents a cumulative retry ACK from becoming a fresh sample.
type roundTripProbe struct {
	active           bool
	sentAt           time.Duration
	endSeq           uint64
	recovering       bool
	recoveryUntilSeq uint64
}

// SetRTOConfig selects timer behavior before the connection starts.
func (c *Client) SetRTOConfig(config model.RTOConfig) {
	if c.handshakeStarted || c.terminated {
		panic("client: RTO configuration must be set before the connection starts")
	}

	c.rto = newRTOEstimator(config)
}

// SetIdleTimeout controls inactivity cleanup independently of retransmission.
// Zero disables cleanup, allowing a long RTO to expire without an earlier idle limit.
func (c *Client) SetIdleTimeout(timeout time.Duration) {
	if timeout < 0 {
		panic("client: idle timeout must not be negative")
	}
	if c.handshakeStarted || c.terminated {
		panic("client: idle timeout must be set before the connection starts")
	}

	c.idleTimeout = timeout
}

// GetRTOState returns measured RTT and the effective timeout, including backoff.
func (c *Client) GetRTOState() model.RTOState {
	return c.rto.state
}

// startRTTProbe times one original segment, including its local queue wait.
// An explicit active flag allows the first transmission to start at virtual time zero.
func (c *Client) startRTTProbe(packet model.Packet) {
	if c.rttProbe.active || c.rttProbe.recovering {
		return
	}
	end, valid := packet.SequenceEnd()
	if !valid {
		return
	}

	c.rttProbe.active = true
	c.rttProbe.sentAt = c.engine.Now()
	c.rttProbe.endSeq = end
}

// observeRTTACK finishes a safe probe when its complete byte range is confirmed.
// An ACK covering retry recovery is skipped, even if it also confirms later data.
func (c *Client) observeRTTACK(ack uint64) {
	probe := &c.rttProbe
	if probe.recovering {
		if ack >= probe.recoveryUntilSeq {
			probe.recovering = false
		}
		return
	}
	if !probe.active || ack < probe.endSeq {
		return
	}

	sample := c.engine.Now() - probe.sentAt
	probe.active = false
	c.rto.observeRTT(sample)
}

// invalidateRTTProbe applies Karn's rule before any retransmission is emitted.
// If data through Seq=3073 was sent, its recovery ACK cannot provide an RTT sample.
func (c *Client) invalidateRTTProbe() {
	c.rttProbe.active = false
	c.rttProbe.recovering = true
	c.rttProbe.recoveryUntilSeq = c.nextSendSeq
}

// sendSyn starts the SYN timer before emitting, so an immediate reply can cancel it.
func (c *Client) sendSyn() {
	packet := GeneratePacket(c.key, model.FlagSYN, 0, 0, 0)
	if c.retrySyn == 0 {
		c.startRTTProbe(packet)
	}
	c.cancelEvent(&c.synTimer)
	c.synTimer = c.engine.ScheduleAfter(c.rto.state.CurrentRTO, c.retransmitSyn)
	c.emit(packet)
}

// retransmitSyn backs off after a missing SYN-ACK and discards the ambiguous RTT.
func (c *Client) retransmitSyn() {
	c.synTimer = 0
	if c.terminated || c.closing || c.handshakeCompleted {
		return
	}
	if c.retrySyn >= model.MAX_RETRANSMISIONS {
		c.ShutdownWithReason(model.TransferSynRetryLimit)
		return
	}

	c.retrySyn++
	c.transfer.synRetransmissions++
	c.invalidateRTTProbe()
	c.congestion.afterSYNRetry()
	c.rto.backOff()
	c.sendSyn()
}

// armDataTimer restarts the current RTO for the oldest unconfirmed fragment.
// New tail segments and duplicate ACKs leave an existing timer untouched.
func (c *Client) armDataTimer() {
	c.cancelEvent(&c.dataTimer)
	c.dataTimer = c.engine.ScheduleAfter(c.rto.state.CurrentRTO, c.retransmitData)
}

// retransmitData retries the oldest fragment and lengthens the next wait.
// For an initial 1 s RTO, losses cause retransmissions at 1 s, 3 s and 7 s.
func (c *Client) retransmitData() {
	c.dataTimer = 0
	if c.terminated || c.closing || !c.pendingSegments.hasPending() {
		return
	}
	if c.oldestSegmentRetransmissions >= model.MAX_RETRANSMISIONS {
		c.ShutdownWithReason(model.TransferDataRetryLimit)
		return
	}

	c.oldestSegmentRetransmissions++
	c.transfer.dataRetransmissions++
	c.congestion.onDataTimeout(c.pendingSegments.oldest(), c.bytesInFlight())
	c.invalidateRTTProbe()
	c.rto.backOff()
	c.armDataTimer()
	c.congestion.recordDataSent(c.engine.Now())
	c.emit(c.pendingSegments.oldest())
}

// armReadTimer starts the separate inactivity limit from the current virtual time.
func (c *Client) armReadTimer() {
	c.cancelEvent(&c.readTimer)
	if c.idleTimeout == 0 {
		return
	}

	c.readTimer = c.engine.ScheduleAfter(c.idleTimeout, func() {
		c.readTimer = 0
		if !c.terminated {
			c.ShutdownWithReason(model.TransferReadTimeout)
		}
	})
}
