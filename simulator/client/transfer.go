package client

import (
	"time"

	"load-balancer/model"
)

// transferMetrics retains timings and retry totals even after ACKs reset the
// protocol's current retry counters or the connection closes.
type transferMetrics struct {
	status        model.TransferStatus
	failureReason model.TransferFailureReason
	startedAt     time.Duration
	finishedAt    time.Duration

	handshakeDuration time.Duration
	payloadStarted    bool
	payloadStartedAt  time.Duration
	payloadDuration   time.Duration

	synRetransmissions  uint64
	dataRetransmissions uint64
}

// recordPayloadStart excludes the initial sender pause from payload duration.
func (c *Client) recordPayloadStart() {
	if !c.transfer.payloadStarted {
		c.transfer.payloadStarted = true
		c.transfer.payloadStartedAt = c.engine.Now()
	}
}

// recordTransferSuccess freezes completion at the final payload ACK, before FIN.
func (c *Client) recordTransferSuccess() {
	c.transfer.status = model.TransferSucceeded
	c.transfer.finishedAt = c.engine.Now()
	if c.transfer.payloadStarted {
		c.transfer.payloadDuration = c.transfer.finishedAt - c.transfer.payloadStartedAt
	}
}

// recordTransferFailure keeps the first outcome; later cleanup cannot change it.
func (c *Client) recordTransferFailure(reason model.TransferFailureReason) {
	if c.transfer.status != model.TransferInProgress {
		return
	}
	if reason == "" {
		reason = model.TransferInterrupted
	}
	c.transfer.status = model.TransferFailed
	c.transfer.failureReason = reason
	c.transfer.finishedAt = c.engine.Now()
}

// GetTransferState reads timings without advancing the model or changing samples.
func (c *Client) GetTransferState() model.TransferState {
	end := c.engine.Now()
	if c.transfer.status != model.TransferInProgress {
		end = c.transfer.finishedAt
	}
	elapsed := time.Duration(0)
	if c.handshakeStarted {
		elapsed = end - c.transfer.startedAt
	}
	return model.TransferState{
		Status:              c.transfer.status,
		FailureReason:       c.transfer.failureReason,
		Elapsed:             elapsed,
		HandshakeCompleted:  c.handshakeCompleted,
		HandshakeDuration:   c.transfer.handshakeDuration,
		PayloadDuration:     c.transfer.payloadDuration,
		SynRetransmissions:  c.transfer.synRetransmissions,
		DataRetransmissions: c.transfer.dataRetransmissions,
	}
}
