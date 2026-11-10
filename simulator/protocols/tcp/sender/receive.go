package sender

import (
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

func (c *Sender) HandlePacket(pkt model.Packet) {
	if c.terminated || pkt.Key != c.key || !pkt.ValidSACKOptions() {
		return
	}
	end, valid := pkt.SequenceEnd()
	invalidControls := pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagFIN != 0
	if !valid || invalidControls {
		return
	}
	// This profile accepts FIN as a separate control segment, without PSH or payload.
	if pkt.Flag&model.FlagFIN != 0 && (pkt.Size != 0 || pkt.Flag&model.FlagPSH != 0) {
		return
	}
	if pkt.Flag&model.FlagRST != 0 {
		c.handleReset(pkt)
		return
	}
	if pkt.Flag&model.FlagSYN != 0 {
		if !c.connection.finSent {
			c.handleSynAck(pkt, end)
		}
		return
	}
	if !c.handshakeCompleted {
		return
	}
	if pkt.Flag&model.FlagACK == 0 || pkt.Size != 0 || pkt.Ack < 1 || pkt.Ack > c.nextSendSeq {
		return
	}
	isFIN := pkt.Flag&model.FlagFIN != 0
	validSequence := pkt.Seq == c.nextReceiveSeq
	duplicateFIN := isFIN && c.connection.peerFIN && end == c.nextReceiveSeq
	if !validSequence && !duplicateFIN {
		return
	}
	c.observeECNFeedback(pkt)
	ackBeforePacket := c.oldestUnackedSeq
	newSACKInformation := false
	// A repeated FIN can carry a newer ACK or SACK report without consuming FIN twice.
	handledDataACK := !c.connection.finSent && (validSequence || duplicateFIN)
	if c.connection.finSent {
		c.acknowledgeFIN(pkt.Ack)
	} else if handledDataACK {
		newSACKInformation = c.handleDataAck(pkt)
	}
	if isFIN && !c.terminated {
		c.handleFin(pkt, end)
	}
	// Report progress and consume FIN before a repair can synchronously get its reply.
	// A callback may already have advanced ACK beyond this packet's position.
	if handledDataACK && c.oldestUnackedSeq == max(ackBeforePacket, pkt.Ack) {
		if c.sack.negotiated {
			c.observeSACKFeedback(newSACKInformation, pkt.Ack > ackBeforePacket)
		}
		if c.sack.negotiated || duplicateFIN {
			c.queueSegment()
		}
	}
}

// handleSynAck confirms our SYN and remembers the peer's independent sequence.
// A repeated SYN-ACK only repeats our ACK; it cannot reset a live data stream.
func (c *Sender) handleSynAck(pkt model.Packet, end uint64) {
	expectedFlags := model.FlagSYN | model.FlagACK
	if !c.handshakeStarted || pkt.Flag & ^(model.FlagECE|model.FlagCWR) != expectedFlags || pkt.Size != 0 || pkt.Ack != 1 {
		return
	}
	if c.handshakeCompleted {
		if pkt.Seq != c.peerInitialSeq {
			return
		}
	} else {
		c.connection.state = model.TCPEstablished
		c.cancelEvent(&c.synTimer)
		c.observeRTTACK(pkt.Ack)
		if c.retrySyn > 0 {
			c.rto.afterSYNRetry()
		}
		c.retrySyn = 0
		c.transfer.handshakeDuration = c.engine.Now() - c.transfer.startedAt
		c.peerInitialSeq = pkt.Seq
		c.nextReceiveSeq = end
		c.oldestUnackedSeq = pkt.Ack
		c.peerWindowEnd = receiveWindowEnd(pkt.Ack, pkt.ReceiveWindowBytes)
		c.peerReceiveWindow = pkt.ReceiveWindowBytes
		c.sack.negotiated = c.sack.enabled && pkt.SACKPermitted
		c.ecn.state.Negotiated = c.ecn.state.Enabled && pkt.Flag&model.FlagECE != 0 && pkt.Flag&model.FlagCWR == 0
		c.congestion.ecnNegotiated = c.ecn.state.Negotiated
		c.handshakeCompleted = true
		if c.source != nil {
			c.source.Start(c.engine.Now())
		}
	}
	if !c.connection.finSent {
		c.armReadTimer()
	}
	c.emit(GeneratePacket(c.key, model.FlagACK, 0, c.nextSendSeq, c.nextReceiveSeq))
	c.queueSegment()
}

// handleDataAck releases every confirmed segment and counts new bytes once.
// ACK=2049 can release both [1,1025) and [1025,2049) at the same time.
func (c *Sender) handleDataAck(pkt model.Packet) (newSACKInformation bool) {
	staleAck := pkt.Ack < c.oldestUnackedSeq
	if staleAck {
		// A reordered ACK can carry a range omitted from a newer four-block report.
		// Keep our current cumulative ACK and rwnd, but retain useful SACK information.
		if c.sack.negotiated {
			newSACKInformation = c.recordSACKReports(c.oldestUnackedSeq, pkt.SACKBlocks)
		}
		return
	}
	c.armReadTimer()

	// A repeated ACK can reopen a window without confirming additional bytes.
	windowUnchanged := pkt.ReceiveWindowBytes == c.peerReceiveWindow
	c.peerWindowEnd = receiveWindowEnd(pkt.Ack, pkt.ReceiveWindowBytes)
	c.peerReceiveWindow = pkt.ReceiveWindowBytes

	// Reports describe buffered data, but only the cumulative ACK releases our payload.
	if c.sack.negotiated {
		newSACKInformation = c.recordSACKReports(pkt.Ack, pkt.SACKBlocks)
	}
	// ACK has not advanced: ACK=1 still asks for data starting at Seq=1.
	if pkt.Ack == c.oldestUnackedSeq {
		if c.sack.negotiated {
			return
		}
		// Treat it as a duplicate only if rwnd is unchanged and data is still pending.
		if pkt.Flag&model.FlagFIN == 0 && windowUnchanged && c.pendingSegments.hasPending() {
			pendingCount := len(c.pendingSegments.segments) - c.pendingSegments.firstPendingIndex

			// The third qualifying duplicate ACK triggers a retry of the oldest segment.
			retryOldest := c.congestion.duplicateACK(
				c.bytesInFlight(), pendingCount,
				c.nextSendSeq, c.oldestUnackedSeq, c.engine.Now(),
				c.ecn.state.Negotiated && pkt.Flag&model.FlagECE != 0,
			)
			if retryOldest {
				c.fastRetransmitOldest(true)
			}
		}
		// A larger rwnd or recovery cwnd may now allow another new segment to be sent.
		if pkt.Flag&model.FlagFIN == 0 {
			c.queueSegment()
		}
		return
	}
	if !c.pendingSegments.hasPending() {
		return
	}

	acknowledgedBytes := int(pkt.Ack - c.oldestUnackedSeq)
	c.observeRTTACK(pkt.Ack)
	c.oldestUnackedSeq = pkt.Ack
	c.remainingPayloadBytesToConfirm -= acknowledgedBytes
	c.acknowledgedPayloadBytes += uint64(acknowledgedBytes)

	c.pendingSegments.acknowledge(pkt.Ack)
	c.forgetECNRetries(pkt.Ack)
	c.observeSender()
	sackRecovery := c.sack.active
	completedSACKRecovery := sackRecovery && pkt.Ack >= c.sack.recoveryEndSeq
	if completedSACKRecovery {
		c.sack.active = false
	}
	response := c.congestion.acknowledge(congestioncontrol.ACK{
		At:              c.engine.Now(),
		NewBytes:        uint64(acknowledgedBytes),
		CumulativeACK:   pkt.Ack,
		FlightBytes:     c.bytesInFlight(),
		SmoothedRTT:     c.rto.state.SmoothedRTT,
		SACKRecovery:    sackRecovery,
		EndSACKRecovery: completedSACKRecovery,
		ECN:             c.ecn.state.Negotiated && pkt.Flag&model.FlagECE != 0,
	})
	c.oldestSegmentRetransmissions = 0

	// NewReno restarts RTO on its first partial ACK, then keeps that deadline.
	// Other ACKs keep the existing rule: restart RTO when new bytes are confirmed.
	if !c.pendingSegments.hasPending() {
		c.cancelEvent(&c.dataTimer)
	} else if !response.PreserveRetransmitTimer || c.dataTimer == 0 {
		c.armDataTimer()
	}
	if c.OnPayloadAcknowledged != nil {
		c.OnPayloadAcknowledged(acknowledgedBytes)
	}
	if response.RetransmitOldest && c.oldestUnackedSeq == pkt.Ack {
		// Accounting and timers are ready before sending; its ACK may arrive synchronously.
		// A payload callback may already have confirmed this gap, making the retry stale.
		c.fastRetransmitOldest(false)
	}
	if !c.sack.negotiated && pkt.Flag&model.FlagFIN == 0 {
		// A combined FIN/ACK must update the receive half before automatic local close.
		c.queueSegment()
	}
	return
}

// receiveWindowEnd bounds the next permitted byte position without wrapping.
func receiveWindowEnd(ack uint64, windowBytes uint16) uint64 {
	maxSequence := ^uint64(0)
	availablePositions := maxSequence - ack
	return ack + min(uint64(windowBytes), availablePositions)
}
