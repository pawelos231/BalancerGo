package congestioncontrol

import (
	"time"

	"load-balancer/model"
)

// cubicPolicy remembers the last congestion window and grows toward it along a cubic curve.
// TCP still detects loss, owns retry timers and sends the actual packets.
type cubicPolicy struct {
	classicWindow
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
}

func newCubicPolicy(config model.CongestionConfig) *cubicPolicy {
	window := newClassicWindow(config)
	initialSegments := float64(window.congestionWindowBytes) / model.SEGMENT_SIZE
	return &cubicPolicy{
		classicWindow:       window,
		windowSegments:      initialSegments,
		priorWindowSegments: initialSegments,
		fastConvergence:     config.CubicFastConvergence,
		resetMaximumAtEpoch: true,
	}
}

// OnACK first completes classic fast recovery, even if the application has no more data.
// Otherwise CUBIC grows only while local demand can use the congestion window.
func (policy *cubicPolicy) OnACK(ack ACK) ACKResponse {
	if policy.activeRecovery {
		// SACK can acknowledge one repaired hole while another still needs recovery.
		if ack.SACKRecovery && !ack.EndSACKRecovery {
			return ACKResponse{}
		}
		policy.StopRecovery()
		policy.setWindowBytes(policy.slowStartThresholdBytes)
		return ACKResponse{}
	}
	if policy.clock.limited {
		return ACKResponse{}
	}

	// This profile selects Reno slow start rather than the separate HyStart++ mechanism.
	// RFC 9438 section 4.10 includes equality with ssthresh in slow start.
	if policy.congestionWindowBytes <= policy.slowStartThresholdBytes {
		increase := min(ack.NewBytes, uint64(model.SEGMENT_SIZE))
		policy.setWindowBytes(addCongestionBytes(policy.congestionWindowBytes, increase))
		if !policy.hasCongestionEvent {
			policy.priorWindowSegments = policy.windowSegments
		}
		return ACKResponse{}
	}
	policy.growCongestionAvoidance(ack)
	return ACKResponse{}
}

// OnDuplicateACK retains classic recovery's bounded one-MSS inflation after the fast retry.
func (policy *cubicPolicy) OnDuplicateACK() {
	if policy.activeRecovery && policy.remainingInflations > 0 {
		policy.remainingInflations--
		policy.setWindowBytes(addCongestionBytes(policy.congestionWindowBytes, model.SEGMENT_SIZE))
	}
}

// OnLoss keeps 70% of flight after a new loss, rather than Reno's 50%.
// With 8192 bytes outstanding, the threshold is floor(8192 * 0.7) = 5734 bytes.
func (policy *cubicPolicy) OnLoss(loss Loss) LossResponse {
	switch loss.Kind {
	case FastRetransmitLoss:
		policy.rememberCongestionWindow()
		policy.slowStartThresholdBytes = cubicLossThreshold(loss.FlightBytes)
		policy.setWindowBytes(policy.slowStartThresholdBytes)
		policy.activeRecovery = true
		policy.remainingInflations = 0

		// Keep CUBIC's 70% reduction; SACK recovery uses Pipe without Reno inflation.
		if !loss.SACKRecovery {
			policy.setWindowBytes(addCongestionBytes(policy.slowStartThresholdBytes, 3*model.SEGMENT_SIZE))
			policy.remainingInflations = uint64(max(loss.PendingSegments-3, 0))
		}
		policy.epochStarted = false
		policy.resetMaximumAtEpoch = false
	case TimeoutLoss:
		policy.StopRecovery()
		if !loss.AlreadyTimedOut {
			policy.rememberCongestionWindow()
			policy.slowStartThresholdBytes = cubicLossThreshold(loss.FlightBytes)
		}
		policy.setWindowBytes(model.SEGMENT_SIZE)
		policy.epochStarted = false
		// RFC 9438 section 4.8: the next CA stage starts with W_max=cwnd_epoch and K=0.
		policy.resetMaximumAtEpoch = true
	case HandshakeLoss:
		policy.restartAfterHandshakeLoss()
		policy.setWindowBytes(policy.congestionWindowBytes)
		policy.epochStarted = false
		policy.resetMaximumAtEpoch = true
	}
	return LossResponse{}
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
	policy.classicWindow.OnIdle()
	policy.setWindowBytes(policy.congestionWindowBytes)
	policy.epochStarted = false
}

func (policy *cubicPolicy) StopRecovery() {
	policy.activeRecovery = false
	policy.remainingInflations = 0
}

func (policy *cubicPolicy) State() State {
	state := policy.classicWindow.State()
	if policy.congestionWindowBytes <= policy.slowStartThresholdBytes {
		state.Phase = "slowStart"
	}
	if policy.activeRecovery {
		state.Phase = "fastRecovery"
	}
	return state
}

// setWindowBytes synchronizes an exact loss/startup window with fractional CA storage.
func (policy *cubicPolicy) setWindowBytes(bytes uint64) {
	policy.congestionWindowBytes = bytes
	policy.windowSegments = float64(bytes) / model.SEGMENT_SIZE
	policy.avoidanceAcknowledgedBytes = 0
}
