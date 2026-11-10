package sender

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// connectionClose keeps FIN retries and teardown timing separate from payload delivery.
// requested drains accepted data; finSent means no more payload can be sent.
type connectionClose struct {
	config             model.CloseConfig
	state              model.TCPState
	outcome            model.CloseOutcome
	requested          bool
	finSent            bool
	finAcked           bool
	peerFIN            bool
	finSeq             uint64
	finRetransmissions uint64
	finRetryDelay      time.Duration
	started            bool
	startedAt          time.Duration
	finishedAt         time.Duration
	sendEvent          engine.EventID
	finTimer           engine.EventID
	waitTimer          engine.EventID
}

func (c *Sender) SetCloseConfig(config model.CloseConfig) {
	if !config.Valid() {
		panic("tcp: invalid TCP closing configuration")
	}
	if c.handshakeStarted || c.terminated {
		panic("tcp: closing configuration must be set before the connection starts")
	}
	c.connection.config = config
}

// IsClosing allows LB routing to survive the FIN exchange and TIME_WAIT.
func (c *Sender) IsClosing() bool {
	return !c.terminated && (c.connection.finSent || c.connection.peerFIN)
}

func (c *Sender) GetConnectionState() model.CloseState {
	close := &c.connection
	var elapsed time.Duration
	if close.started {
		end := c.engine.Now()
		if c.terminated {
			end = close.finishedAt
		}
		elapsed = end - close.startedAt
	}
	return model.CloseState{
		State:              close.state,
		Outcome:            close.outcome,
		FINRetransmissions: close.finRetransmissions,
		Elapsed:            elapsed,
	}
}

func (c *Sender) beginClosing() {
	if !c.connection.started {
		c.connection.started = true
		c.connection.startedAt = c.engine.Now()
	}
}

// startFINWhenDrained sends FIN once all payload has been acknowledged.
func (c *Sender) startFINWhenDrained() {
	close := &c.connection

	// Start only for a requested close, before FIN has been sent.
	if c.terminated || close.finSent || !close.requested {
		return
	}

	// Wait until all payload is sent and acknowledged.
	if c.unsentBytes() != 0 || c.pendingSegments.hasPending() {
		return
	}

	// Reserve one sequence number for FIN; retries reuse it.
	close.finSent = true
	close.finSeq = c.nextSendSeq
	c.nextSendSeq++

	// Use the current RTO as the initial FIN retry delay.
	close.finRetryDelay = c.rto.state.CurrentRTO

	// Wait for our FIN's ACK; LAST_ACK applies if the peer already sent FIN.
	close.state = model.TCPFinWait1
	if close.peerFIN {
		close.state = model.TCPLastAck
	}

	// Stop data timers and recovery; FIN has its own retry timer.
	c.cancelEvent(&c.readTimer)
	c.cancelEvent(&c.dataTimer)
	c.cancelEvent(&c.sendEvent)
	c.cancelEvent(&c.ecn.holdEvent)
	c.congestion.clearLossTracking()

	// Schedule FIN at the current virtual time.
	close.sendEvent = c.engine.ScheduleAfter(0, func() {
		close.sendEvent = 0

		if !c.terminated && !close.finAcked {
			// Schedule before sending so an immediate ACK can cancel the retry.
			c.scheduleFINRetransmission()
			c.emitFIN()
		}
	})
}

func (c *Sender) emitFIN() {
	c.emit(GeneratePacket(
		c.key, model.FlagFIN|model.FlagACK, 0,
		c.connection.finSeq, c.nextReceiveSeq,
	))
}

func (c *Sender) scheduleFINRetransmission() {
	c.cancelEvent(&c.connection.finTimer)
	c.connection.finTimer = c.engine.ScheduleAfter(c.connection.finRetryDelay, c.retryFIN)
}

// retryFIN backs off its own timer without changing payload RTO measurements or counters.
func (c *Sender) retryFIN() {
	close := &c.connection
	close.finTimer = 0
	if c.terminated || close.finAcked {
		return
	}

	if close.finRetransmissions >= uint64(close.config.MaxFINRetries) {
		c.finishConnection(model.CloseFINRetryLimit, model.TransferCloseTimeout)
		return
	}

	close.finRetransmissions++
	if c.rto.config.ExponentialBackoff {
		maximum := c.rto.config.MaximumTimeout
		if close.finRetryDelay >= maximum-close.finRetryDelay {
			close.finRetryDelay = maximum
		} else {
			close.finRetryDelay *= 2
		}
	}

	c.scheduleFINRetransmission() // An immediate ACK must be able to cancel the retry.
	c.emitFIN()
}

// acknowledgeFIN confirms our FIN and advances the closing state.
func (c *Sender) acknowledgeFIN(ack uint64) {
	close := &c.connection

	// Accept only the first ACK that confirms our FIN.
	if !close.finSent || close.finAcked || ack != close.finSeq+1 {
		return
	}

	// Our FIN arrived, so stop retrying it.
	close.finAcked = true
	c.cancelEvent(&close.finTimer)

	switch close.state {
	case model.TCPLastAck:
		// The peer closed first; this ACK completes our reply.
		c.finishConnection(model.CloseGraceful, model.TransferClosedEarly)

	case model.TCPFinWait1, model.TCPClosing:
		if close.peerFIN {
			// Peer FIN already arrived; keep state to ACK any duplicate FINs.
			c.enterTimeWait()
		} else {
			// Our FIN is confirmed; wait for the peer to finish sending.
			close.state = model.TCPFinWait2

			// Close with a timeout if the peer's FIN never arrives.
			close.waitTimer = c.engine.ScheduleAfter(close.config.FinWaitTimeout, func() {
				close.waitTimer = 0

				c.finishConnection(model.CloseWaitTimeout, model.TransferCloseTimeout)
			})
		}
	}
}

// handleFin accepts the peer's end once; duplicate FINs repeat ACK without consuming another byte.
func (c *Sender) handleFin(pkt model.Packet, end uint64) {
	close := &c.connection
	if pkt.Size != 0 || pkt.Flag&model.FlagACK == 0 || pkt.Ack > c.nextSendSeq {
		return
	}
	if close.peerFIN {
		if end != c.nextReceiveSeq {
			return
		}
		if close.state == model.TCPTimeWait {
			c.enterTimeWait()
		}
		c.emitCloseACK()
		return
	}

	if pkt.Seq != c.nextReceiveSeq {
		return
	}

	c.beginClosing()
	close.peerFIN = true
	c.nextReceiveSeq = end

	if close.finSent {
		if close.finAcked {
			c.enterTimeWait()
		} else {
			close.state = model.TCPClosing
		}
	} else {
		// The peer has ended its sending half; accepted client payload can still finish.
		close.state = model.TCPCloseWait
		close.requested = true
		c.emitCloseACK()
		c.queueSegment()
		c.startFINWhenDrained()
		return
	}
	c.emitCloseACK()
}

func (c *Sender) emitCloseACK() {
	c.emit(GeneratePacket(c.key, model.FlagACK, 0, c.nextSendSeq, c.nextReceiveSeq))
}

// enterTimeWait keeps TCP state long enough to acknowledge a repeated peer FIN.
func (c *Sender) enterTimeWait() {
	close := &c.connection
	close.state = model.TCPTimeWait
	c.cancelEvent(&close.finTimer)
	c.cancelEvent(&close.waitTimer)
	close.waitTimer = c.engine.ScheduleAfter(2*close.config.MSL, func() {
		close.waitTimer = 0
		c.finishConnection(model.CloseGraceful, model.TransferClosedEarly)
	})
}

func (c *Sender) finishConnection(outcome model.CloseOutcome, reason model.TransferFailureReason) {
	if c.terminated {
		return
	}
	c.beginClosing()
	c.connection.outcome = outcome
	c.ShutdownWithReason(reason)
}

// handleReset accepts only a reset for the current sequence position and state.
// In SYN_SENT, ACK must cover our SYN; established connections also permit a bare RST.
func (c *Sender) handleReset(pkt model.Packet) {
	invalidFlags := pkt.Flag&(model.FlagSYN|model.FlagFIN|model.FlagPSH) != 0
	if pkt.Size != 0 || invalidFlags {
		return
	}
	if !c.handshakeCompleted {
		if !c.handshakeStarted || pkt.Flag&model.FlagACK == 0 || pkt.Ack != c.nextSendSeq {
			return
		}
	} else {
		if pkt.Seq != c.nextReceiveSeq {
			return
		}
		if pkt.Flag&model.FlagACK != 0 && (pkt.Ack < 1 || pkt.Ack > c.nextSendSeq) {
			return
		}
	}
	c.finishConnection(model.CloseReset, model.TransferReset)
}
