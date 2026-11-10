package congestioncontrol

import (
	"time"

	"load-balancer/model"
)

// ClassicWindow holds the slow-start and congestion-avoidance rules shared by Reno variants.
// Their loss policies decide when to use these rules and when recovery consumes an ACK.
type ClassicWindow struct {
	MSSBytes                   uint64 // One segment's payload limit, fixed for this connection.
	StartupWindowBytes         uint64
	CongestionWindowBytes      uint64
	MaximumWindowBytes         uint64
	SlowStartThresholdBytes    uint64
	AvoidanceAcknowledgedBytes uint64
}

// NewClassicWindow builds shared state from settings already checked by the factory.
func NewClassicWindow(config model.CongestionConfig, mssBytes int) ClassicWindow {
	segmentBytes := uint64(mssBytes)
	maximum := model.MaxCongestionWindowBytes
	if config.MaximumWindowSegments > 0 {
		maximum = uint64(config.MaximumWindowSegments) * segmentBytes
	}
	initial := min(uint64(config.InitialWindowSegments)*segmentBytes, maximum)
	return ClassicWindow{
		MSSBytes:                segmentBytes,
		StartupWindowBytes:      initial,
		CongestionWindowBytes:   initial,
		MaximumWindowBytes:      maximum,
		SlowStartThresholdBytes: config.InitialSlowStartThresholdBytes,
	}
}

// OnACK grows slow start by at most one MSS per new ACK, then grows one MSS per window.
// A cumulative 4096-byte ACK increases slow start by one MSS, not four.
// https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1
func (window *ClassicWindow) OnACK(ack ACK) ACKResponse {
	if ack.ECN {
		return ACKResponse{}
	}
	newBytes := ack.NewBytes
	if window.CongestionWindowBytes < window.SlowStartThresholdBytes {
		increase := min(newBytes, window.MSSBytes)
		window.SetWindowBytes(AddCongestionBytes(window.CongestionWindowBytes, increase))
		return ACKResponse{}
	}

	window.AvoidanceAcknowledgedBytes = AddCongestionBytes(window.AvoidanceAcknowledgedBytes, newBytes)
	if window.AvoidanceAcknowledgedBytes >= window.CongestionWindowBytes {
		window.AvoidanceAcknowledgedBytes -= window.CongestionWindowBytes
		window.SetWindowBytes(AddCongestionBytes(window.CongestionWindowBytes, window.MSSBytes))
	}
	return ACKResponse{}
}

// ECN halves the usable window without inventing a missing packet or fast recovery.
// A small window can reach one MSS; TCP then applies RFC 3168's timer backoff.
// https://www.rfc-editor.org/rfc/rfc3168.html#section-6.1.2
func (window *ClassicWindow) OnECN(ecn ECN) {
	window.SlowStartThresholdBytes = max(ecn.FlightBytes/2, 2*window.MSSBytes)
	window.SetWindowBytes(max(window.CongestionWindowBytes/2, window.MSSBytes))
	window.AvoidanceAcknowledgedBytes = 0
}

// RestartAfterLoss starts at one MSS and halves flight only for a new loss indication.
func (window *ClassicWindow) RestartAfterLoss(flight uint64, alreadyTimedOut bool) {
	if !alreadyTimedOut {
		window.SlowStartThresholdBytes = max(flight/2, 2*window.MSSBytes)
	}
	window.SetWindowBytes(window.MSSBytes)
	window.AvoidanceAcknowledgedBytes = 0
}

// RestartAfterHandshakeLoss also reduces the window used by future idle restarts.
func (window *ClassicWindow) RestartAfterHandshakeLoss() {
	window.StartupWindowBytes = window.MSSBytes
	window.SetWindowBytes(window.MSSBytes)
	window.AvoidanceAcknowledgedBytes = 0
}

// OnIdle uses the connection's effective initial window, including a prior SYN retry.
func (window *ClassicWindow) OnIdle() {
	window.SetWindowBytes(min(window.CongestionWindowBytes, window.StartupWindowBytes))
	window.AvoidanceAcknowledgedBytes = 0
}

// SetWindowBytes keeps the configured ceiling after growth, loss and recovery.
// For example, a one-segment ceiling keeps cwnd=1024 after a successful ACK.
func (window *ClassicWindow) SetWindowBytes(bytes uint64) {
	maximum := window.MaximumWindowBytes
	if maximum == 0 {
		maximum = model.MaxCongestionWindowBytes
	}
	window.CongestionWindowBytes = min(bytes, maximum)
}

func (window *ClassicWindow) State() State {
	phase := "congestionAvoidance"
	if window.CongestionWindowBytes < window.SlowStartThresholdBytes {
		phase = "slowStart"
	}

	return State{
		WindowBytes:    window.CongestionWindowBytes,
		ThresholdBytes: window.SlowStartThresholdBytes,
		Phase:          phase, LimitsNewData: true,
	}
}

// OnSent does nothing: our Reno/Tahoe profiles control new sends through cwnd and ACKs.
// A paced algorithm such as BBR can use send time/size to space later packets out.
// Real stacks may add pacing to window-based algorithms too; these profiles do not.
func (*ClassicWindow) OnSent(time.Duration, int) {}

// Reno/Tahoe keep their existing ACK growth even when workload or rwnd limits sending.
func (*ClassicWindow) OnSenderState(SenderState) {}

// Tahoe does not react to duplicate ACKs.
func (*ClassicWindow) OnDuplicateACK() {}

// StopRecovery lets Tahoe satisfy Policy without keeping a fast-recovery state.
func (*ClassicWindow) StopRecovery() {}

// AddCongestionBytes saturates a window or ACK counter instead of wrapping it.
func AddCongestionBytes(current, increase uint64) uint64 {
	if increase >= model.MaxCongestionWindowBytes-current {
		return model.MaxCongestionWindowBytes
	}
	return current + increase
}
