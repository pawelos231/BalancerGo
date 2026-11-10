package sender

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

type senderECN struct {
	state         model.ECNState
	holdUntil     time.Duration
	holdEvent     engine.EventID
	holdRTO       time.Duration     // Repeated one-MSS congestion backs off the sending rate.
	holdBeyondRun bool              // A deadline beyond the horizon must not release data at the horizon.
	retryRanges   []model.SACKBlock // Repairs can become the oldest fragment after ACK progress.
}

// SetECNEnabled offers classic ECN on SYN; it takes effect only if the peer accepts.
func (c *Sender) SetECNEnabled(enabled bool) {
	if c.handshakeStarted || c.terminated {
		panic("tcp: ECN must be configured before the connection starts")
	}
	c.ecn.state.Enabled = enabled
}

func (c *Sender) GetECNState() model.ECNState { return c.ecn.state }

// Callers may configure ECN and congestion control in either order before starting.
func (c *Sender) validateECNProfile() {
	if c.ecn.state.Enabled && c.congestion.config.Mode == model.CongestionNone {
		panic("tcp: ECN requires congestion control")
	}
}

func (c *Sender) synFlags() model.Flag {
	flags := model.FlagSYN
	if c.ecn.state.Enabled {
		flags |= model.FlagECE | model.FlagCWR
	}
	return flags
}

// observeECNFeedback uses the pre-ACK flight so an ACK covering the marked packet
// can reduce the window even when it drains the flight. ECN never requests a retry.
// RFC 3168 section 6.1.2: https://www.rfc-editor.org/rfc/rfc3168.html#section-6.1.2
func (c *Sender) observeECNFeedback(pkt model.Packet) {
	if !c.ecn.state.Negotiated {
		return
	}
	// Reordered feedback cannot reset the persistent-congestion backoff or counters.
	if pkt.Ack < c.oldestUnackedSeq {
		return
	}
	if pkt.Flag&model.FlagECE == 0 {
		c.ecn.holdRTO = 0
		return
	}
	c.ecn.state.ReceivedECECount++
	control := &c.congestion
	minimumWindow := control.policy.State().WindowBytes <= uint64(c.mssBytes)
	// Even a drained flight can receive repeated ECE before the next source release.
	refreshingHold := minimumWindow && c.unsentBytes() > 0
	if !c.pendingSegments.hasPending() && !refreshingHold {
		return
	}
	alreadyReduced := control.reductionEndSeq != 0 && pkt.Ack <= control.reductionEndSeq
	if minimumWindow {
		c.holdNewDataForECN(!alreadyReduced)
	}
	// This boundary is separate from loss.untilSeq: ECN must not block fast repair.
	if alreadyReduced {
		return
	}
	control.policy.OnECN(congestioncontrol.ECN{
		At: c.engine.Now(), FlightBytes: c.bytesInFlight(),
	})
	control.recordReduction(c.nextSendSeq)
	c.ecn.state.ECNResponseCount++
}

// At one MSS a window cannot shrink further. Use the current RTO as a new-data
// delay, even if this ACK removes the last pending segment; no loss is fabricated.
func (c *Sender) holdNewDataForECN(newCongestion bool) {
	if c.ecn.holdRTO == 0 {
		c.ecn.holdRTO = c.rto.state.CurrentRTO
	} else if newCongestion {
		// RFC 9438 section 4.6 calls out exponential backoff for persistent ECE.
		maximum := c.rto.config.MaximumTimeout
		if c.ecn.holdRTO >= maximum-c.ecn.holdRTO {
			c.ecn.holdRTO = maximum
		} else {
			c.ecn.holdRTO *= 2
		}
	}
	c.ecn.holdBeyondRun = c.ecn.holdRTO > c.engine.EndTime()-c.engine.Now()
	if !c.ecn.holdBeyondRun {
		c.ecn.holdUntil = c.engine.Now() + c.ecn.holdRTO
	}
	c.cancelEvent(&c.sendEvent)
	c.cancelEvent(&c.ecn.holdEvent)
	if c.pendingSegments.hasPending() {
		c.cancelEvent(&c.dataTimer)
		c.dataTimer = c.engine.ScheduleAfter(c.ecn.holdRTO, c.retransmitData)
	}
	c.ecn.holdEvent = c.engine.ScheduleAfter(c.ecn.holdRTO, func() {
		c.ecn.holdEvent = 0
		if !c.terminated {
			c.queueSegment()
		}
	})
}

// Keep buffered originals clean: retries must be Not-ECT and must never replay CWR.
func (c *Sender) decorateNewPayload(packet *model.Packet) {
	if !c.ecn.state.Negotiated {
		return
	}
	packet.ECN.Codepoint = model.ECT0
	if c.congestion.cwrPending {
		packet.Flag |= model.FlagCWR
		c.congestion.cwrPending = false
		c.ecn.state.SentCWRCount++
	}
}

// Remember actual repaired bytes: a dropped repair is a new congestion event
// under RFC 3168 section 6.1.2, even after ACK progress resets the retry counter.
func (c *Sender) recordECNRetry(packet model.Packet) {
	if c.ecn.state.Negotiated {
		c.ecn.retryRanges = append(c.ecn.retryRanges, model.SACKBlock{
			Start: packet.Seq, End: packet.Seq + uint64(packet.Size),
		})
	}
}

func (c *Sender) wasECNRetransmitted(sequence uint64) bool {
	for _, block := range c.ecn.retryRanges {
		if sequence >= block.Start && sequence < block.End {
			return true
		}
	}
	return false
}

func (c *Sender) forgetECNRetries(ack uint64) {
	kept := c.ecn.retryRanges[:0]
	for _, block := range c.ecn.retryRanges {
		if block.End > ack {
			block.Start = max(block.Start, ack)
			kept = append(kept, block)
		}
	}
	c.ecn.retryRanges = kept
}
