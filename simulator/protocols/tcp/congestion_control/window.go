package congestioncontrol

import (
	"time"

	"load-balancer/model"
)

// classicWindow holds the slow-start and congestion-avoidance rules shared by Reno variants.
// Their loss policies decide when to use these rules and when recovery consumes an ACK.
type classicWindow struct {
	mssBytes                   uint64 // One segment's payload limit, fixed for this connection.
	startupWindowBytes         uint64
	congestionWindowBytes      uint64
	maximumWindowBytes         uint64
	slowStartThresholdBytes    uint64
	avoidanceAcknowledgedBytes uint64
}

func newClassicWindow(config model.CongestionConfig, mssBytes int) classicWindow {
	segmentBytes := uint64(mssBytes)
	maximum := model.MaxCongestionWindowBytes
	if config.MaximumWindowSegments > 0 {
		maximum = uint64(config.MaximumWindowSegments) * segmentBytes
	}
	initial := min(uint64(config.InitialWindowSegments)*segmentBytes, maximum)
	return classicWindow{
		mssBytes:                segmentBytes,
		startupWindowBytes:      initial,
		congestionWindowBytes:   initial,
		maximumWindowBytes:      maximum,
		slowStartThresholdBytes: config.InitialSlowStartThresholdBytes,
	}
}

// OnACK grows slow start by at most one MSS per new ACK, then grows one MSS per window.
// A cumulative 4096-byte ACK increases slow start by one MSS, not four.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1
func (window *classicWindow) OnACK(ack ACK) ACKResponse {
	if ack.ECN {
		return ACKResponse{}
	}
	newBytes := ack.NewBytes
	if window.congestionWindowBytes < window.slowStartThresholdBytes {
		increase := min(newBytes, window.mssBytes)
		window.setWindowBytes(addCongestionBytes(window.congestionWindowBytes, increase))
		return ACKResponse{}
	}

	window.avoidanceAcknowledgedBytes = addCongestionBytes(window.avoidanceAcknowledgedBytes, newBytes)
	if window.avoidanceAcknowledgedBytes >= window.congestionWindowBytes {
		window.avoidanceAcknowledgedBytes -= window.congestionWindowBytes
		window.setWindowBytes(addCongestionBytes(window.congestionWindowBytes, window.mssBytes))
	}
	return ACKResponse{}
}

// ECN halves the usable window without inventing a missing packet or fast recovery.
// A small window can reach one MSS; TCP then applies RFC 3168's timer backoff.
// https://www.rfc-editor.org/rfc/rfc3168.html#section-6.1.2
func (window *classicWindow) OnECN(ecn ECN) {
	window.slowStartThresholdBytes = max(ecn.FlightBytes/2, 2*window.mssBytes)
	window.setWindowBytes(max(window.congestionWindowBytes/2, window.mssBytes))
	window.avoidanceAcknowledgedBytes = 0
}

// restartAfterLoss starts at one MSS and halves flight only for a new loss indication.
func (window *classicWindow) restartAfterLoss(flight uint64, alreadyTimedOut bool) {
	if !alreadyTimedOut {
		window.slowStartThresholdBytes = max(flight/2, 2*window.mssBytes)
	}
	window.setWindowBytes(window.mssBytes)
	window.avoidanceAcknowledgedBytes = 0
}

// restartAfterHandshakeLoss also reduces the window used by future idle restarts.
func (window *classicWindow) restartAfterHandshakeLoss() {
	window.startupWindowBytes = window.mssBytes
	window.setWindowBytes(window.mssBytes)
	window.avoidanceAcknowledgedBytes = 0
}

// OnIdle uses the connection's effective initial window, including a prior SYN retry.
func (window *classicWindow) OnIdle() {
	window.setWindowBytes(min(window.congestionWindowBytes, window.startupWindowBytes))
	window.avoidanceAcknowledgedBytes = 0
}

// setWindowBytes keeps the configured ceiling after growth, loss and recovery.
// For example, a one-segment ceiling keeps cwnd=1024 after a successful ACK.
func (window *classicWindow) setWindowBytes(bytes uint64) {
	maximum := window.maximumWindowBytes
	if maximum == 0 {
		maximum = model.MaxCongestionWindowBytes
	}
	window.congestionWindowBytes = min(bytes, maximum)
}

func (window *classicWindow) State() State {
	phase := "congestionAvoidance"
	if window.congestionWindowBytes < window.slowStartThresholdBytes {
		phase = "slowStart"
	}

	return State{
		WindowBytes:    window.congestionWindowBytes,
		ThresholdBytes: window.slowStartThresholdBytes,
		Phase:          phase, LimitsNewData: true,
	}
}

// OnSent does nothing: our Reno/Tahoe profiles control new sends through cwnd and ACKs.
// A paced algorithm such as BBR can use send time/size to space later packets out.
// Real stacks may add pacing to window-based algorithms too; these profiles do not.
func (*classicWindow) OnSent(time.Duration, int) {}

// Reno/Tahoe keep their existing ACK growth even when workload or rwnd limits sending.
func (*classicWindow) OnSenderState(SenderState) {}

// Tahoe does not react to duplicate ACKs.
func (*classicWindow) OnDuplicateACK() {}

// StopRecovery lets Tahoe satisfy Policy without keeping a fast-recovery state.
func (*classicWindow) StopRecovery() {}

// addCongestionBytes saturates a window or ACK counter instead of wrapping it.
func addCongestionBytes(current, increase uint64) uint64 {
	if increase >= model.MaxCongestionWindowBytes-current {
		return model.MaxCongestionWindowBytes
	}
	return current + increase
}
