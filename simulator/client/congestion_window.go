package client

import (
	"time"

	"load-balancer/model"
)

// classicWindow holds the slow-start and congestion-avoidance rules shared by Reno/Tahoe.
// Their loss policies decide when to use these rules and when recovery consumes an ACK.
type classicWindow struct {
	startupWindowBytes         uint64
	congestionWindowBytes      uint64
	slowStartThresholdBytes    uint64
	avoidanceAcknowledgedBytes uint64
}

func newClassicWindow(config model.CongestionConfig) classicWindow {
	initial := uint64(config.InitialWindowSegments) * model.SEGMENT_SIZE
	return classicWindow{
		startupWindowBytes:      initial,
		congestionWindowBytes:   initial,
		slowStartThresholdBytes: config.InitialSlowStartThresholdBytes,
	}
}

// onACK grows slow start by at most one MSS per new ACK, then grows one MSS per window.
// A cumulative 4096-byte ACK increases slow start by one MSS, not four.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1
func (window *classicWindow) onACK(ack congestionACK) {
	newBytes := ack.newBytes
	if window.congestionWindowBytes < window.slowStartThresholdBytes {
		increase := min(newBytes, uint64(model.SEGMENT_SIZE))
		window.congestionWindowBytes = addCongestionBytes(window.congestionWindowBytes, increase)
		return
	}

	window.avoidanceAcknowledgedBytes = addCongestionBytes(window.avoidanceAcknowledgedBytes, newBytes)
	if window.avoidanceAcknowledgedBytes >= window.congestionWindowBytes {
		window.avoidanceAcknowledgedBytes -= window.congestionWindowBytes
		window.congestionWindowBytes = addCongestionBytes(window.congestionWindowBytes, model.SEGMENT_SIZE)
	}
}

// restartAfterLoss starts at one MSS and halves flight only for a new loss indication.
func (window *classicWindow) restartAfterLoss(flight uint64, alreadyTimedOut bool) {
	if !alreadyTimedOut {
		window.slowStartThresholdBytes = max(flight/2, 2*model.SEGMENT_SIZE)
	}
	window.congestionWindowBytes = model.SEGMENT_SIZE
	window.avoidanceAcknowledgedBytes = 0
}

// restartAfterHandshakeLoss also reduces the window used by future idle restarts.
func (window *classicWindow) restartAfterHandshakeLoss() {
	window.startupWindowBytes = model.SEGMENT_SIZE
	window.congestionWindowBytes = model.SEGMENT_SIZE
	window.avoidanceAcknowledgedBytes = 0
}

// onIdle uses the connection's effective initial window, including a prior SYN retry.
func (window *classicWindow) onIdle() {
	window.congestionWindowBytes = min(window.congestionWindowBytes, window.startupWindowBytes)
	window.avoidanceAcknowledgedBytes = 0
}

func (window *classicWindow) state() congestionPolicyState {
	phase := "congestionAvoidance"
	if window.congestionWindowBytes < window.slowStartThresholdBytes {
		phase = "slowStart"
	}

	return congestionPolicyState{
		windowBytes:    window.congestionWindowBytes,
		thresholdBytes: window.slowStartThresholdBytes,
		phase:          phase, limitsNewData: true,
	}
}

// onSent does nothing: our Reno/Tahoe profiles control new sends through cwnd and ACKs.
// A paced algorithm such as BBR can use send time/size to space later packets out.
// Real stacks may add pacing to window-based algorithms too; these profiles do not.
func (*classicWindow) onSent(time.Duration, int) {}

// Reno/Tahoe keep their existing ACK growth even when workload or rwnd limits sending.
func (*classicWindow) onSenderState(congestionSenderState) {}

// we need to satisfy the polocy Tahoe does not react to duplicate ACK's
func (*classicWindow) onDuplicateACK() {}

// stopRecovery lets Tahoe satisfy congestionPolicy without keeping a fast-recovery state.
func (*classicWindow) stopRecovery() {}
