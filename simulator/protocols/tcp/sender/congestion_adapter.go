package sender

import (
	"time"

	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
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

	// ECN shares window reductions with loss, while loss keeps its own repair guard.
	ecnNegotiated   bool
	reductionEndSeq uint64
	cwrPending      bool
}

// newCongestionControl chooses one policy before any segment is sent.
func newCongestionControl(config model.CongestionConfig) congestionControl {
	return newCongestionControlWithMSS(config, model.DefaultMSSBytes)
}

func newCongestionControlWithMSS(config model.CongestionConfig, mssBytes int) congestionControl {
	return congestionControl{
		config: config,
		policy: congestioncontrol.NewWithMSS(config, mssBytes),
	}
}

// SetCongestionConfig selects sender behavior before the connection starts.
func (c *Sender) SetCongestionConfig(config model.CongestionConfig) {
	c.SetCongestionConfigWithMSS(config, c.mssBytes)
}

// SetCongestionConfigWithMSS applies both settings together before traffic starts.
// For MSS 536, a valid two-MSS threshold is 1072 bytes rather than 2048.
func (c *Sender) SetCongestionConfigWithMSS(config model.CongestionConfig, mssBytes int) {
	if c.handshakeStarted || c.terminated {
		panic("tcp: congestion configuration must be set before the connection starts")
	}
	control := newCongestionControlWithMSS(config, mssBytes)
	c.mssBytes = mssBytes
	c.congestion = control
}

// GetCongestionState combines algorithm state with TCP's current flight and counters.
func (c *Sender) GetCongestionState() model.CongestionState {
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
func (c *Sender) bytesInFlight() uint64 {
	if !c.handshakeCompleted || c.connection.finSent || c.terminated {
		return 0
	}
	return c.nextSendSeq - c.oldestUnackedSeq
}

func (c *Sender) availableCongestionWindowBytes() uint64 {
	window := c.congestion.policy.State()
	if !window.LimitsNewData {
		return model.MaxCongestionWindowBytes
	}

	flight := c.bytesInFlight()
	if c.sack.active {
		flight = c.sack.pipeBytes
	}
	if flight >= window.WindowBytes {
		return 0
	}

	return window.WindowBytes - flight
}

// acknowledge handles an ACK that confirms new data and resets the duplicate ACK count.
// Example: ACK=2049 confirms bytes through 2048, so a retried segment ending at 1025 is done.
// The selected algorithm then updates cwnd using the newly confirmed bytes.
func (control *congestionControl) acknowledge(ack congestioncontrol.ACK) congestioncontrol.ACKResponse {
	if control.timedOutSegmentEnd != 0 && ack.CumulativeACK >= control.timedOutSegmentEnd {
		control.timedOutSegmentEnd = 0
	}
	control.loss.duplicateACKs = 0
	if control.loss.untilSeq != 0 && ack.CumulativeACK >= control.loss.untilSeq {
		control.loss.untilSeq = 0
	}
	return control.policy.OnACK(ack)
}

// observeSender tells the policy about available payload and its next workload deadline.
func (c *Sender) observeSender() {
	var readyAt time.Duration
	if c.source != nil {
		readyAt, _ = c.source.NextSendAt()
	}
	c.congestion.policy.OnSenderState(congestioncontrol.SenderState{
		At:                 c.engine.Now(),
		ReadyAt:            readyAt,
		HasData:            c.unsentBytes() > 0,
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
	retryingRetransmission bool,
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
		CumulativeACK:   packet.Seq,
		NextSendSeq:     nextSendSeq,
		AlreadyTimedOut: alreadyTimedOut,
		// Keep this flight's previous ECN threshold reduction. The real timeout
		// still collapses cwnd, retransmits, and backs off its timer normally.
		AlreadyReduced: !retryingRetransmission && control.ecnNegotiated && packet.Seq < control.reductionEndSeq,
	})
	control.recordReduction(nextSendSeq)
}

func (control *congestionControl) recordReduction(nextSendSeq uint64) {
	if control.ecnNegotiated {
		control.reductionEndSeq = nextSendSeq
		control.cwrPending = true
	}
}

// afterSYNRetry reports handshake loss without changing the shared SYN/RTO machinery.
func (control *congestionControl) afterSYNRetry(now time.Duration) {
	control.policy.OnLoss(congestioncontrol.Loss{At: now, Kind: congestioncontrol.HandshakeLoss})
}

// restartAfterIdle notifies the policy only when a send is due after more than one RTO.
func (control *congestionControl) restartAfterIdle(now, currentRTO time.Duration) {
	if control.hasSentData && now-control.lastDataSentAt > currentRTO {
		before := control.policy.State().WindowBytes
		control.policy.OnIdle()
		if control.ecnNegotiated && control.policy.State().WindowBytes < before {
			control.cwrPending = true
		}
	}
}

// recordDataSent includes retries, which end the idle interval too.
func (control *congestionControl) recordDataSent(now time.Duration, payloadBytes int) {
	control.lastDataSentAt = now
	control.hasSentData = true
	control.policy.OnSent(now, payloadBytes)
}

// clearLossTracking resets duplicateACKs and untilSeq, then stops recovery.
func (control *congestionControl) clearLossTracking() {
	control.loss.duplicateACKs = 0
	control.loss.untilSeq = 0
	control.policy.StopRecovery()
}
