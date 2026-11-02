package client

import "load-balancer/model"

func (c *Client) HandlePacket(pkt model.Packet) {
	if c.terminated || pkt.Key != c.key {
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
	if c.connection.finSent {
		c.acknowledgeFIN(pkt.Ack)
	} else if validSequence {
		c.handleDataAck(pkt)
	}
	if isFIN && !c.terminated {
		c.handleFin(pkt, end)
	}
}

// handleSynAck confirms our SYN and remembers the peer's independent sequence.
// A repeated SYN-ACK only repeats our ACK; it cannot reset a live data stream.
func (c *Client) handleSynAck(pkt model.Packet, end uint64) {
	expectedFlags := model.FlagSYN | model.FlagACK
	if !c.handshakeStarted || pkt.Flag != expectedFlags || pkt.Size != 0 || pkt.Ack != 1 {
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
		c.handshakeCompleted = true
		if c.streamRequested {
			c.updateNextSendTime()
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
func (c *Client) handleDataAck(pkt model.Packet) {
	if pkt.Flag&model.FlagACK == 0 || pkt.Size != 0 {
		return
	}

	validPeerPosition := pkt.Seq == c.nextReceiveSeq
	staleAck := pkt.Ack < c.oldestUnackedSeq
	acknowledgesUnsentData := pkt.Ack > c.nextSendSeq
	if !validPeerPosition || staleAck || acknowledgesUnsentData {
		return
	}
	c.armReadTimer()

	// A repeated ACK can reopen a window without confirming additional bytes.
	windowUnchanged := pkt.ReceiveWindowBytes == c.peerReceiveWindow
	c.peerWindowEnd = receiveWindowEnd(pkt.Ack, pkt.ReceiveWindowBytes)
	c.peerReceiveWindow = pkt.ReceiveWindowBytes
	// ACK has not advanced: ACK=1 still asks for data starting at Seq=1.
	if pkt.Ack == c.oldestUnackedSeq {
		// Treat it as a duplicate only if rwnd is unchanged and data is still pending.
		if pkt.Flag&model.FlagFIN == 0 && windowUnchanged && c.pendingSegments.hasPending() {
			pendingCount := len(c.pendingSegments.segments) - c.pendingSegments.firstPendingIndex

			// The third qualifying duplicate ACK triggers a retry of the oldest segment.
			retryOldest := c.congestion.duplicateACK(
				c.bytesInFlight(), pendingCount,
				c.nextSendSeq, c.oldestUnackedSeq, c.engine.Now(),
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
	c.observeSender()
	response := c.congestion.acknowledge(
		uint64(acknowledgedBytes), pkt.Ack, c.bytesInFlight(),
		c.engine.Now(), c.rto.state.SmoothedRTT,
	)
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
	if pkt.Flag&model.FlagFIN == 0 {
		// A combined FIN/ACK must update the receive half before automatic local close.
		c.queueSegment()
	}
}

// receiveWindowEnd bounds the next permitted byte position without wrapping.
func receiveWindowEnd(ack uint64, windowBytes uint16) uint64 {
	maxSequence := ^uint64(0)
	availablePositions := maxSequence - ack
	return ack + min(uint64(windowBytes), availablePositions)
}
