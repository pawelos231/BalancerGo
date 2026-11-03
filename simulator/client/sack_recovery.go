package client

import (
	congestioncontrol "load-balancer/client/congestion_control"
	"load-balancer/model"
)

// sackSender owns selective loss recovery; the congestion policy still owns cwnd.
// Range starts are inclusive; ends are exclusive, like Packet.SequenceEnd().
type sackSender struct {
	enabled    bool
	negotiated bool
	board      sackScoreboard

	active           bool
	recoveryEndSeq   uint64 // A cumulative ACK must reach this point to finish recovery.
	retransmittedEnd uint64 // Ordinary recovery retries proceed forwards from this point.
	rescueAfterSeq   uint64 // ACK must pass this point before one rescue retry is allowed.
	duplicateACKs    uint64
	pipeBytes        uint64 // Refreshed on ACK; each new transmission adds its bytes.
	dispatching      bool
}

// SetSACKEnabled offers selective acknowledgments during the next handshake.
// It is off by default, preserving the original cumulative-ACK profile.
func (c *Client) SetSACKEnabled(enabled bool) {
	if c.handshakeStarted || c.terminated {
		panic("client: SACK must be configured before the connection starts")
	}
	c.sack.enabled = enabled
}

// validateSACKProfile keeps unsupported combinations out of the low-level API too.
func (c *Client) validateSACKProfile() {
	if !c.sack.enabled {
		return
	}
	mode := c.congestion.config.Mode
	supported := mode == model.CongestionReno || mode == model.CongestionNewReno ||
		mode == model.CongestionCubic
	if !supported || !c.congestion.config.FastRetransmit {
		panic("client: SACK requires Reno, NewReno or CUBIC with fast retransmit enabled")
	}
}

// recordSACKReports retains received ranges and refreshes the recovery flight estimate.
// ack is our current cumulative position, even when the report came in an older ACK.
func (c *Client) recordSACKReports(ack uint64, blocks []model.SACKBlock) bool {
	changed := c.sack.board.update(ack, c.nextSendSeq, blocks)
	if c.sack.active {
		c.sack.pipeBytes = c.sack.board.pipe(ack, c.nextSendSeq, c.sack.retransmittedEnd)
	}
	return changed
}

// observeSACKFeedback enters recovery only when new reports provide enough evidence.
// Three small reports also count, even if their bytes total less than three MSS.
func (c *Client) observeSACKFeedback(newInformation, advancingACK bool) {
	if !c.sack.negotiated || c.terminated || !c.pendingSegments.hasPending() {
		return
	}
	if advancingACK {
		c.sack.duplicateACKs = 0
	}
	if c.sack.active || !newInformation {
		return
	}
	c.sack.duplicateACKs++
	c.congestion.loss.duplicateACKs = c.sack.duplicateACKs
	canRecover := c.oldestUnackedSeq >= c.sack.recoveryEndSeq
	enoughEvidence := c.sack.duplicateACKs >= 3 || c.sack.board.isLost(c.oldestUnackedSeq)
	if !canRecover || !enoughEvidence {
		return
	}

	response := c.congestion.policy.OnLoss(congestioncontrol.Loss{
		At:              c.engine.Now(),
		Kind:            congestioncontrol.FastRetransmitLoss,
		FlightBytes:     c.bytesInFlight(),
		CumulativeACK:   c.oldestUnackedSeq,
		NextSendSeq:     c.nextSendSeq,
		PendingSegments: len(c.pendingSegments.segments) - c.pendingSegments.firstPendingIndex,
		SACKRecovery:    true,
	})
	if response.SuppressFastRetransmit {
		return
	}

	// Freeze the old flight boundary before a retry can synchronously produce an ACK.
	c.sack.active = true
	c.sack.recoveryEndSeq = c.nextSendSeq
	c.sack.retransmittedEnd = c.oldestUnackedSeq
	c.sack.pipeBytes = c.sack.board.pipe(c.oldestUnackedSeq, c.nextSendSeq, c.oldestUnackedSeq)
	c.congestion.loss.untilSeq = c.nextSendSeq
	firstHole, found := c.sack.board.nextHole(c.oldestUnackedSeq, c.nextSendSeq, false)
	if found {
		c.sack.rescueAfterSeq = firstHole.End
		c.retransmitSACKHole(firstHole, false)
	}
}

// sendSACKRepairs follows RFC 6675 NextSeg: lost data, new data, then fallback retries.
// The existing send event handles new data, including workload gaps and pacing.
func (c *Client) sendSACKRepairs() {
	if c.sack.dispatching {
		return
	}
	c.sack.dispatching = true
	defer func() { c.sack.dispatching = false }()

	for c.sack.active && !c.terminated && !c.connection.finSent {
		if c.availableCongestionWindowBytes() < model.SEGMENT_SIZE {
			return
		}
		start := max(c.oldestUnackedSeq, c.sack.retransmittedEnd)
		highestReported := c.oldestUnackedSeq
		if count := len(c.sack.board.ranges); count > 0 {
			highestReported = c.sack.board.ranges[count-1].End
		}

		// Prefer a gap with evidence of loss over sending any more new bytes.
		hole, found := c.sack.board.nextHole(start, highestReported, true)
		if found {
			c.retransmitSACKHole(hole, false)
			continue
		}
		if c.nextNewSegmentWithinRun && c.nextPayloadSize() > 0 {
			return
		}

		// With no new data available, cautiously fill the next gap below a SACK block.
		hole, found = c.sack.board.nextHole(start, highestReported, false)
		if found {
			c.retransmitSACKHole(hole, false)
			continue
		}
		if c.oldestUnackedSeq <= c.sack.rescueAfterSeq {
			return
		}
		hole, found = c.sack.board.lastHole(c.oldestUnackedSeq, c.nextSendSeq)
		if !found {
			return
		}

		// One rescue retransmission can sustain ACKs when the tail of the flight was lost.
		c.sack.rescueAfterSeq = c.sack.recoveryEndSeq
		c.retransmitSACKHole(hole, true)
		return
	}
}

// retransmitSACKHole sends only the selected missing bytes, retaining the original buffer.
// For example, a report covering [1537,2049) leaves only [1025,1537) to retry.
func (c *Client) retransmitSACKHole(hole model.SACKBlock, rescue bool) {
	if hole.Start == c.oldestUnackedSeq {
		if c.oldestSegmentRetransmissions >= model.MAX_RETRANSMISIONS {
			c.ShutdownWithReason(model.TransferDataRetryLimit)
			return
		}
		c.oldestSegmentRetransmissions++
	}
	if !rescue {
		c.sack.retransmittedEnd = hole.End
	}
	c.transfer.dataRetransmissions++
	c.congestion.loss.fastRetransmissions++
	c.sack.pipeBytes += hole.End - hole.Start
	c.invalidateRTTProbe()
	c.congestion.recordDataSent(c.engine.Now(), int(hole.End-hole.Start))

	// RFC 6675 section 6 permits restarting RTO on each recovery retransmission.
	c.armDataTimer()
	packet := GeneratePacket(
		c.key, model.FlagPSH|model.FlagACK,
		int(hole.End-hole.Start), hole.Start, c.nextReceiveSeq,
	)
	c.emit(packet)
}

// resetSACKAfterTimeout distrusts old reports: the receiver may have discarded them.
// The old flight must be cumulatively ACKed before starting another fast recovery.
func (c *Client) resetSACKAfterTimeout() {
	c.sack.board.ranges = nil
	c.sack.active = false
	c.sack.duplicateACKs = 0
	c.sack.recoveryEndSeq = c.nextSendSeq
	c.sack.retransmittedEnd = 0
	c.sack.rescueAfterSeq = 0
	c.sack.pipeBytes = 0
}
