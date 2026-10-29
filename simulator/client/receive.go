package client

import "load-balancer/model"

func (c *Client) HandlePacket(pkt model.Packet) {
	if c.terminated || pkt.Key != c.key {
		return
	}
	c.armReadTimer()
	end, valid := pkt.SequenceEnd()
	if !valid || pkt.Flag&model.FlagRST != 0 {
		return
	}
	if pkt.Flag&model.FlagSYN != 0 {
		if !c.closing {
			c.handleSynAck(pkt, end)
		}
		return
	}
	if !c.handshakeCompleted {
		return
	}
	// FIN|ACK closes the peer side; its ACK must not count as a data ACK.
	if pkt.Flag&model.FlagFIN != 0 {
		c.handleFin(pkt, end)
		return
	}
	if !c.closing {
		c.handleDataAck(pkt)
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
		c.cancelEvent(&c.synTimer)
		c.retrySyn = 0
		c.transfer.handshakeDuration = c.engine.Now() - c.transfer.startedAt
		c.peerInitialSeq = pkt.Seq
		c.nextReceiveSeq = end
		c.oldestUnackedSeq = pkt.Ack
		c.peerWindowEnd = receiveWindowEnd(pkt.Ack, pkt.ReceiveWindowBytes)
		c.handshakeCompleted = true
	}
	c.emit(GeneratePacket(c.key, model.FlagACK, 0, c.nextSendSeq, c.nextReceiveSeq))
	c.queueSegment()
}

// handleDataAck confirms only new bytes and retains the unacknowledged suffix.
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

	// A repeated ACK can reopen a window without confirming additional bytes.
	c.peerWindowEnd = receiveWindowEnd(pkt.Ack, pkt.ReceiveWindowBytes)
	if pkt.Ack == c.oldestUnackedSeq {
		c.queueSegment()
		return
	}
	if !c.waitingForAck {
		return
	}

	acknowledgedBytes := int(pkt.Ack - c.oldestUnackedSeq)
	c.oldestUnackedSeq = pkt.Ack
	c.remainingPayloadBytesToConfirm -= acknowledgedBytes
	c.acknowledgedPayloadBytes += uint64(acknowledgedBytes)

	// Keep only the missing part, so a timeout never resends confirmed bytes.
	c.lastSent.Seq = pkt.Ack
	c.lastSent.Size -= acknowledgedBytes
	c.currentSegmentRetransmissions = 0
	c.cancelEvent(&c.dataTimer)
	if c.lastSent.Size == 0 {
		c.waitingForAck = false
	} else {
		c.armDataTimer()
	}

	if c.OnPayloadAcknowledged != nil {
		c.OnPayloadAcknowledged(acknowledgedBytes)
	}
	c.queueSegment()
}

// receiveWindowEnd bounds the next permitted byte position without wrapping.
func receiveWindowEnd(ack uint64, windowBytes uint16) uint64 {
	maxSequence := ^uint64(0)
	availablePositions := maxSequence - ack
	return ack + min(uint64(windowBytes), availablePositions)
}

// handleFin validates byte positions while retaining the existing simple close.
// Full closing states and FIN retransmission remain a later step.
func (c *Client) handleFin(pkt model.Packet, end uint64) {
	if pkt.Size != 0 || pkt.Seq != c.nextReceiveSeq {
		return
	}
	if c.closing {
		if pkt.Flag&model.FlagACK == 0 || pkt.Ack != c.nextSendSeq {
			return
		}
	} else if pkt.Flag&model.FlagACK != 0 {
		if pkt.Ack < c.oldestUnackedSeq || pkt.Ack > c.nextSendSeq {
			return
		}
	}
	c.nextReceiveSeq = end
	c.ShutdownWithReason(model.TransferRemoteClosed)
}
