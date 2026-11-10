package cubic

import (
	"time"

	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// cubicPolicy remembers the last congestion window and grows toward it along a cubic curve.
// TCP still detects loss, owns retry timers and sends the actual packets.
type cubicPolicy struct {
	congestioncontrol.ClassicWindow
	clock cubicClock

	windowSegments          float64 // Fractional growth survives until it amounts to another byte.
	previousMaximumSegments float64 // W_max: the remembered window before a congestion reduction.
	priorWindowSegments     float64 // cwnd_prior: the window before the most recent reduction.
	fastConvergence         bool
	hasCongestionEvent      bool

	epochStarted        bool
	epochActiveAt       time.Duration
	returnToMaximumSecs float64 // K: active seconds needed to return to W_max from this epoch's window.
	renoWindowSegments  float64 // W_est: the competing Reno-friendly window estimate.
	renoAlpha           float64
	resetMaximumAtEpoch bool

	activeRecovery      bool
	remainingInflations uint64
	recoveryWindowBytes uint64
}

// New builds a CUBIC policy from settings already validated by the factory.
func New(config model.CongestionConfig, mssBytes int) congestioncontrol.Policy {
	return newCubicPolicy(config, mssBytes)
}

func newCubicPolicy(config model.CongestionConfig, mssBytes int) *cubicPolicy {
	window := congestioncontrol.NewClassicWindow(config, mssBytes)
	initialSegments := float64(window.CongestionWindowBytes) / float64(window.MSSBytes)
	return &cubicPolicy{
		ClassicWindow:       window,
		windowSegments:      initialSegments,
		priorWindowSegments: initialSegments,
		fastConvergence:     config.CubicFastConvergence,
		resetMaximumAtEpoch: true,
	}
}

// OnACK first completes classic fast recovery, even if the application has no more data.
// Otherwise CUBIC grows only while local demand can use the congestion window.
func (policy *cubicPolicy) OnACK(ack congestioncontrol.ACK) congestioncontrol.ACKResponse {
	if policy.activeRecovery {
		// SACK can acknowledge one repaired hole while another still needs recovery.
		if ack.SACKRecovery && !ack.EndSACKRecovery {
			return congestioncontrol.ACKResponse{}
		}
		policy.StopRecovery()
		policy.setWindowBytes(policy.recoveryWindowBytes)
		return congestioncontrol.ACKResponse{}
	}
	if ack.ECN {
		return congestioncontrol.ACKResponse{}
	}
	if policy.clock.limited {
		return congestioncontrol.ACKResponse{}
	}

	// This profile selects Reno slow start rather than the separate HyStart++ mechanism.
	// RFC 9438 section 4.10 includes equality with ssthresh in slow start.
	if policy.CongestionWindowBytes <= policy.SlowStartThresholdBytes {
		increase := min(ack.NewBytes, policy.MSSBytes)
		policy.setWindowBytes(congestioncontrol.AddCongestionBytes(
			policy.CongestionWindowBytes, increase,
		))
		if !policy.hasCongestionEvent {
			policy.priorWindowSegments = policy.windowSegments
		}
		return congestioncontrol.ACKResponse{}
	}
	policy.growCongestionAvoidance(ack)
	return congestioncontrol.ACKResponse{}
}

// OnDuplicateACK retains classic recovery's bounded one-MSS inflation after the fast retry.
func (policy *cubicPolicy) OnDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.setWindowBytes(congestioncontrol.AddCongestionBytes(
			policy.CongestionWindowBytes, policy.MSSBytes,
		))
	}
}

// OnLoss keeps 70% of flight after a new loss, rather than Reno's 50%.
// With 8192 bytes outstanding, the threshold is floor(8192 * 0.7) = 5734 bytes.
func (policy *cubicPolicy) OnLoss(loss congestioncontrol.Loss) congestioncontrol.LossResponse {
	switch loss.Kind {
	case congestioncontrol.FastRetransmitLoss:
		if !loss.AlreadyReduced {
			policy.rememberCongestionWindow()
			policy.SlowStartThresholdBytes = cubicLossThreshold(loss.FlightBytes, policy.MSSBytes)
			policy.recoveryWindowBytes = policy.SlowStartThresholdBytes
		} else {
			policy.recoveryWindowBytes = policy.CongestionWindowBytes
		}
		policy.setWindowBytes(policy.recoveryWindowBytes)
		policy.activeRecovery = true
		policy.remainingInflations = 0

		// Keep CUBIC's 70% reduction; SACK recovery uses Pipe without Reno inflation.
		if !loss.SACKRecovery {
			policy.setWindowBytes(congestioncontrol.AddCongestionBytes(
				policy.recoveryWindowBytes, 3*policy.MSSBytes,
			))
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
		policy.epochStarted = false
		policy.resetMaximumAtEpoch = false
	case congestioncontrol.TimeoutLoss:
		policy.StopRecovery()
		if !loss.AlreadyTimedOut && !loss.AlreadyReduced {
			policy.rememberCongestionWindow()
			policy.SlowStartThresholdBytes = cubicLossThreshold(loss.FlightBytes, policy.MSSBytes)
		}
		policy.setWindowBytes(policy.MSSBytes)
		policy.epochStarted = false
		// RFC 9438 section 4.8: the next CA stage starts with W_max=cwnd_epoch and K=0.
		policy.resetMaximumAtEpoch = true
	case congestioncontrol.HandshakeLoss:
		policy.RestartAfterHandshakeLoss()
		policy.setWindowBytes(policy.CongestionWindowBytes)
		policy.epochStarted = false
		policy.resetMaximumAtEpoch = true
	}
	return congestioncontrol.LossResponse{}
}

// ECE is a congestion event with CUBIC's beta=0.7, without loss recovery.
// Remember W_max and restart the curve just as for fast loss; ECN can reduce cwnd
// below the two-MSS ssthresh floor, down to one MSS under persistent congestion.
// https://www.rfc-editor.org/rfc/rfc9438.html#section-4.6
func (policy *cubicPolicy) OnECN(ecn congestioncontrol.ECN) {
	policy.rememberCongestionWindow()
	policy.SlowStartThresholdBytes = cubicLossThreshold(ecn.FlightBytes, policy.MSSBytes)
	policy.setWindowBytes(max(cubicRetainedBytes(policy.CongestionWindowBytes), policy.MSSBytes))
	if policy.activeRecovery {
		policy.recoveryWindowBytes = policy.CongestionWindowBytes
	}
	policy.epochStarted = false
	policy.resetMaximumAtEpoch = false
}

// rememberCongestionWindow applies optional fast convergence before reducing the window.
// A competing flow can lower a previous 100-MSS maximum to 80 * 0.85 = 68 MSS.
func (policy *cubicPolicy) rememberCongestionWindow() {
	window := policy.windowSegments
	maximum := window
	if policy.fastConvergence && window < policy.previousMaximumSegments {
		maximum *= (1 + cubicBeta) / 2
	}
	policy.previousMaximumSegments = maximum
	policy.priorWindowSegments = window
	policy.hasCongestionEvent = true
}

// OnIdle retains the common conservative restart, then starts a fresh growth epoch.
func (policy *cubicPolicy) OnIdle() {
	policy.ClassicWindow.OnIdle()
	policy.setWindowBytes(policy.CongestionWindowBytes)
	policy.epochStarted = false
}

func (policy *cubicPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
}

func (policy *cubicPolicy) State() congestioncontrol.State {
	state := policy.ClassicWindow.State()
	if policy.CongestionWindowBytes <= policy.SlowStartThresholdBytes {
		state.Phase = "slowStart"
	}
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}

// setWindowBytes synchronizes an exact loss/startup window with fractional CA storage.
func (policy *cubicPolicy) setWindowBytes(bytes uint64) {
	policy.ClassicWindow.SetWindowBytes(bytes)
	policy.windowSegments = float64(policy.CongestionWindowBytes) / float64(policy.MSSBytes)
	policy.AvoidanceAcknowledgedBytes = 0
}
