package client

import (
	"time"

	congestioncontrol "load-balancer/client/congestion_control"
	"load-balancer/model"
)

// congestionControl passes TCP events to the separate congestion_control package.
// It tracks ACKs and retries; the selected algorithm owns its window and recovery state.
type congestionControl struct {
	config model.CongestionConfig
	policy congestioncontrol.Policy // The selected congestion-control algorithm, such as Reno or Tahoe.
	loss   lossDetection

	lastDataSentAt time.Duration
	hasSentData    bool

	// A partial ACK can trim a retry without changing the original segment's end.
	// Only RTO sets this marker; fast retransmit must not consume the first RTO reduction.
	timedOutSegmentEnd uint64
}

// newCongestionControl chooses one policy before any segment is sent.
func newCongestionControl(config model.CongestionConfig) congestionControl {
	return congestionControl{
		config: config,
		policy: congestioncontrol.New(config),
	}
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
	window := control.policy.State()
	return model.CongestionState{
		Mode:                    control.config.Mode,
		CongestionWindowBytes:   window.WindowBytes,
		SlowStartThresholdBytes: window.ThresholdBytes,
		BytesInFlight:           c.bytesInFlight(),
		DuplicateACKs:           control.loss.duplicateACKs,
		FastRetransmissions:     control.loss.fastRetransmissions,
		Phase:                   window.Phase,
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
	window := c.congestion.policy.State()
	if !window.LimitsNewData {
		return model.MaxCongestionWindowBytes
	}

	flight := c.bytesInFlight()
	if flight >= window.WindowBytes {
		return 0
	}

	return window.WindowBytes - flight
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
	control.policy.OnACK(congestioncontrol.ACK{
		At:          now,
		NewBytes:    newBytes,
		SmoothedRTT: smoothedRTT,
	})
}

// observeSender tells the policy about local demand without exposing the client itself.
func (c *Client) observeSender() {
	c.congestion.policy.OnSenderState(congestioncontrol.SenderState{
		At:                 c.engine.Now(),
		ReadyAt:            c.nextNewSegmentAt,
		HasData:            c.unsentPayloadBytes > 0,
		FlightBytes:        c.bytesInFlight(),
		ReceiveWindowBytes: uint64(c.peerReceiveWindow),
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
	control.policy.OnLoss(congestioncontrol.Loss{
		At:              now,
		Kind:            congestioncontrol.TimeoutLoss,
		FlightBytes:     flight,
		AlreadyTimedOut: alreadyTimedOut,
	})
}

// afterSYNRetry reports handshake loss without changing the shared SYN/RTO machinery.
func (control *congestionControl) afterSYNRetry(now time.Duration) {
	control.policy.OnLoss(congestioncontrol.Loss{At: now, Kind: congestioncontrol.HandshakeLoss})
}

// restartAfterIdle notifies the policy only when a send is due after more than one RTO.
func (control *congestionControl) restartAfterIdle(now, currentRTO time.Duration) {
	if control.hasSentData && now-control.lastDataSentAt > currentRTO {
		control.policy.OnIdle()
	}
}

// recordDataSent includes retries, which end the idle interval too.
func (control *congestionControl) recordDataSent(now time.Duration, payloadBytes int) {
	control.lastDataSentAt = now
	control.hasSentData = true
	control.policy.OnSent(now, payloadBytes)
}

// clearLossTracking abandons pending loss detection but preserves total retry metrics.
func (control *congestionControl) clearLossTracking() {
	control.loss.duplicateACKs = 0
	control.loss.untilSeq = 0
	control.policy.StopRecovery()
}
