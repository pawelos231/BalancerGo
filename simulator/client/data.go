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
	if c.terminated || c.closing || c.streamRequested {
		return
	}
	c.streamRequested = true
	c.remainingPayloadBytesToConfirm = dataSize
	c.gap = gap
	if c.handshakeCompleted {
		c.queueSegment()
	}
}

func (c *Client) queueSegment() {
	canSend := c.handshakeCompleted && c.streamRequested
	busy := c.waitingForAck || c.sendEvent != 0
	if c.terminated || c.closing || !canSend || busy {
		return
	}
	if c.remainingPayloadBytesToConfirm == 0 {
		c.finishStream()
		return
	}
	if c.nextPayloadSize() == 0 {
		// A closed window pauses new data. This profile has no persist probes yet.
		return
	}

	c.sendEvent = c.engine.ScheduleAfter(c.gap, func() {
		c.sendEvent = 0
		if c.terminated || c.closing {
			return
		}

		// Recheck credit: an ACK may have changed the window during the gap.
		payloadBytes := c.nextPayloadSize()
		if payloadBytes == 0 {
			return
		}

		// Data also acknowledges the handshake if its final standalone ACK was lost.
		c.lastSent = GeneratePacket(
			c.key,
			model.FlagPSH|model.FlagACK,
			payloadBytes,
			c.nextSendSeq,
			c.nextReceiveSeq,
		)
		c.nextSendSeq += uint64(payloadBytes)
		c.recordPayloadStart()
		c.waitingForAck = true
		c.currentSegmentRetransmissions = 0
		c.armDataTimer()
		c.emit(c.lastSent)
	})
}

// nextPayloadSize fits the next segment inside the backend's advertised window.
func (c *Client) nextPayloadSize() int {
	if c.peerWindowEnd <= c.nextSendSeq {
		return 0
	}

	availableBytes := c.peerWindowEnd - c.nextSendSeq
	payloadLimit := min(c.remainingPayloadBytesToConfirm, model.SEGMENT_SIZE)
	return int(min(uint64(payloadLimit), availableBytes))
}

func (c *Client) armDataTimer() {
	c.cancelEvent(&c.dataTimer)
	c.dataTimer = c.engine.ScheduleAfter(model.SEGMENT_TIMEOUT, c.retransmitData)
}

func (c *Client) retransmitData() {
	c.dataTimer = 0
	if c.terminated || c.closing || !c.waitingForAck {
		return
	}
	if c.currentSegmentRetransmissions >= model.MAX_RETRANSMISIONS {
		c.ShutdownWithReason(model.TransferDataRetryLimit)
		return
	}
	c.currentSegmentRetransmissions++
	c.transfer.dataRetransmissions++
	c.armDataTimer()
	c.emit(c.lastSent)
}
