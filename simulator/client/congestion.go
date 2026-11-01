package client

import (
	"time"

	"load-balancer/model"
)

// congestionControl connects TCP's ACK/loss bookkeeping to the selected algorithm.
// The policy changes the window; TCP still owns segments, timers and retransmissions.
type congestionControl struct {
	config model.CongestionConfig
	policy congestionPolicy // The selected congestion-control algorithm, such as Reno or Tahoe.
	loss   lossDetection

	lastDataSentAt time.Duration
	hasSentData    bool

	// A partial ACK can trim a retry without changing the original segment's end.
	// Only RTO sets this marker; fast retransmit must not consume the first RTO reduction.
	timedOutSegmentEnd uint64
}

// newCongestionControl chooses one policy before any segment is sent.
func newCongestionControl(config model.CongestionConfig) congestionControl {
	validMode := config.Mode == model.CongestionNone ||
		config.Mode == model.CongestionReno ||
		config.Mode == model.CongestionTahoe ||
		config.Mode == model.CongestionCubic

	// Start with at least one segment, without exceeding the byte limit.
	maximumInitialSegments := model.MaxCongestionWindowBytes / model.SEGMENT_SIZE
	validInitialWindow := config.InitialWindowSegments >= 1 &&
		uint64(config.InitialWindowSegments) <= maximumInitialSegments

	// Keep the slow-start threshold between two segments and the byte limit.
	minimumThresholdBytes := uint64(2 * model.SEGMENT_SIZE)
	validThreshold := config.InitialSlowStartThresholdBytes >= minimumThresholdBytes &&
		config.InitialSlowStartThresholdBytes <= model.MaxCongestionWindowBytes

	// Mode none must leave fast retransmit disabled.
	validRetransmit := !config.FastRetransmit ||
		config.Mode != model.CongestionNone

	if !validMode || !validInitialWindow || !validThreshold || !validRetransmit {
		panic("client: invalid congestion configuration")
	}

	var policy congestionPolicy
	switch config.Mode {
	case model.CongestionReno:
		policy = &renoPolicy{classicWindow: newClassicWindow(config)}
	case model.CongestionTahoe:
		policy = &tahoePolicy{classicWindow: newClassicWindow(config)}
	case model.CongestionCubic:
		policy = newCubicPolicy(config)
	case model.CongestionNone:
		policy = unlimitedPolicy{}
	}
	return congestionControl{config: config, policy: policy}
}

// SetCongestionConfig selects sender behavior before the connection starts.
func (c *Client) SetCongestionConfig(config model.CongestionConfig) {
	if c.handshakeStarted || c.terminated {
		panic("client: congestion configuration must be set before the connection starts")
	}
	c.congestion = newCongestionControl(config)
}

// GetCongestionState combines algorithm state with TCP's current flight and counters.
func (c *Client) GetCongestionState() model.CongestionState {
	control := &c.congestion
	window := control.policy.state()
	return model.CongestionState{
		Mode:                    control.config.Mode,
		CongestionWindowBytes:   window.windowBytes,
		SlowStartThresholdBytes: window.thresholdBytes,
		BytesInFlight:           c.bytesInFlight(),
		DuplicateACKs:           control.loss.duplicateACKs,
		FastRetransmissions:     control.loss.fastRetransmissions,
		Phase:                   window.phase,
	}
}

// bytesInFlight counts sent payload that has not yet been cumulatively ACKed.
// SYN and FIN reserve sequence positions but do not count as payload in flight.
func (c *Client) bytesInFlight() uint64 {
	if !c.handshakeCompleted || c.closing || c.terminated {
		return 0
	}
	return c.nextSendSeq - c.oldestUnackedSeq
}

func (c *Client) availableCongestionWindowBytes() uint64 {
	window := c.congestion.policy.state()
	if !window.limitsNewData {
		return model.MaxCongestionWindowBytes
	}

	flight := c.bytesInFlight()
	if flight >= window.windowBytes {
		return 0
	}

	return window.windowBytes - flight
}

// acknowledge handles an ACK that confirms new data and resets the duplicate ACK count.
// Example: ACK=2049 confirms bytes through 2048, so a retried segment ending at 1025 is done.
// The selected algorithm then updates cwnd using the newly confirmed bytes.
func (control *congestionControl) acknowledge(
	newBytes, ack uint64,
	now, smoothedRTT time.Duration,
) {
	if control.timedOutSegmentEnd != 0 && ack >= control.timedOutSegmentEnd {
		control.timedOutSegmentEnd = 0
	}
	control.loss.duplicateACKs = 0
	if control.loss.untilSeq != 0 && ack >= control.loss.untilSeq {
		control.loss.untilSeq = 0
	}
	control.policy.onACK(congestionACK{at: now, newBytes: newBytes, smoothedRTT: smoothedRTT})
}

// observeSender tells the policy about local demand without exposing the client itself.
func (c *Client) observeSender() {
	c.congestion.policy.onSenderState(congestionSenderState{
		at:                 c.engine.Now(),
		readyAt:            c.nextNewSegmentAt,
		hasData:            c.unsentPayloadBytes > 0,
		flightBytes:        c.bytesInFlight(),
		receiveWindowBytes: uint64(c.peerReceiveWindow),
	})
}

// onDataTimeout keeps RTO accounting separate from a prior fast retransmission.
// Retrying [513,1025) after a partial ACK still belongs to original [1,1025).
func (control *congestionControl) onDataTimeout(
	packet model.Packet,
	flight, nextSendSeq uint64,
	now time.Duration,
) {
	control.loss.duplicateACKs = 0
	control.loss.untilSeq = nextSendSeq
	alreadyTimedOut := control.timedOutSegmentEnd != 0
	if !alreadyTimedOut {
		control.timedOutSegmentEnd = packet.Seq + uint64(packet.Size)
	}
	control.policy.onLoss(congestionLoss{
		at: now, kind: timeoutLoss, flightBytes: flight, alreadyTimedOut: alreadyTimedOut,
	})
}

// afterSYNRetry reports handshake loss without changing the shared SYN/RTO machinery.
func (control *congestionControl) afterSYNRetry(now time.Duration) {
	control.policy.onLoss(congestionLoss{at: now, kind: handshakeLoss})
}

// restartAfterIdle notifies the policy only when a send is due after more than one RTO.
func (control *congestionControl) restartAfterIdle(now, currentRTO time.Duration) {
	if control.hasSentData && now-control.lastDataSentAt > currentRTO {
		control.policy.onIdle()
	}
}

// recordDataSent includes retries, which end the idle interval too.
func (control *congestionControl) recordDataSent(now time.Duration, payloadBytes int) {
	control.lastDataSentAt = now
	control.hasSentData = true
	control.policy.onSent(now, payloadBytes)
}

// clearLossTracking abandons pending loss detection but preserves total retry metrics.
func (control *congestionControl) clearLossTracking() {
	control.loss.duplicateACKs = 0
	control.loss.untilSeq = 0
	control.policy.stopRecovery()
}

// addCongestionBytes saturates a window or ACK counter instead of wrapping it.
func addCongestionBytes(current, increase uint64) uint64 {
	if increase >= model.MaxCongestionWindowBytes-current {
		return model.MaxCongestionWindowBytes
	}
	return current + increase
}
