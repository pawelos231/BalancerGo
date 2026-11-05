package congestioncontrol

import (
	"math"
	"time"

	"load-balancer/model"
)

const (
	cubicScale = 0.4
	cubicBeta  = 0.7
	cubicAlpha = 3 * (1 - cubicBeta) / (1 + cubicBeta)
)

// startEpoch anchors the curve to the actual window at the start of congestion avoidance.
// Epoch validity is explicit because zero is a valid virtual timestamp.
func (policy *cubicPolicy) startEpoch(now time.Duration) {
	if policy.resetMaximumAtEpoch {
		policy.previousMaximumSegments = policy.windowSegments
		policy.resetMaximumAtEpoch = false
	}
	policy.epochStarted = true
	policy.epochActiveAt = policy.clock.activeAt(now)
	policy.returnToMaximumSecs = math.Cbrt(
		(policy.previousMaximumSegments - policy.windowSegments) / cubicScale,
	)
	policy.renoWindowSegments = policy.windowSegments
	policy.renoAlpha = cubicAlpha
}

// growCongestionAvoidance implements RFC 9438 sections 4.2 through 4.5.
// It compares the cubic curve at t with W_est, but aims at the curve one RTT ahead.
func (policy *cubicPolicy) growCongestionAvoidance(ack ACK) {
	if !policy.epochStarted {
		policy.startEpoch(ack.At)
	}
	elapsed := (policy.clock.activeAt(ack.At) - policy.epochActiveAt).Seconds()
	elapsed = max(elapsed, 0)
	window := policy.windowSegments

	// Fractional segments count: an ACK of 512 bytes contributes half a 1024-byte MSS.
	if policy.renoWindowSegments >= policy.priorWindowSegments {
		policy.renoAlpha = 1
	}
	segmentsACKed := float64(ack.NewBytes) / model.SEGMENT_SIZE
	policy.renoWindowSegments += policy.renoAlpha * segmentsACKed / window
	if policy.renoWindowSegments >= policy.priorWindowSegments {
		policy.renoAlpha = 1
	}

	curveNow := policy.curveWindow(elapsed)
	if curveNow < policy.renoWindowSegments {
		policy.windowSegments = max(window, policy.renoWindowSegments)
	} else {
		// A zero RTT from simultaneous virtual events uses the engine's 1 ns time quantum.
		rtt := max(ack.SmoothedRTT, time.Nanosecond).Seconds()
		target := policy.curveWindow(elapsed + rtt)
		target = max(window, min(target, 1.5*window))
		// RFC 4.4/4.5 adds this per new ACK; only W_est above weights segmentsACKed.
		policy.windowSegments = window + (target-window)/window
	}
	policy.storeFractionalWindow()
}

func (policy *cubicPolicy) curveWindow(elapsed float64) float64 {
	distance := elapsed - policy.returnToMaximumSecs
	return cubicScale*distance*distance*distance + policy.previousMaximumSegments
}

// storeFractionalWindow keeps sub-byte growth while publishing safe integer byte limits.
func (policy *cubicPolicy) storeFractionalWindow() {
	maximumBytes := policy.maximumWindowBytes
	if maximumBytes == 0 {
		maximumBytes = model.MaxCongestionWindowBytes
	}
	maximum := float64(maximumBytes) / model.SEGMENT_SIZE
	policy.windowSegments = max(1, min(policy.windowSegments, maximum))

	bytes := policy.windowSegments * model.SEGMENT_SIZE
	if bytes >= float64(maximumBytes) {
		policy.congestionWindowBytes = maximumBytes
		return
	}

	policy.congestionWindowBytes = uint64(bytes)
}

func cubicLossThreshold(flight uint64) uint64 {
	return max(cubicRetainedBytes(flight), 2*model.SEGMENT_SIZE)
}

func cubicRetainedBytes(bytes uint64) uint64 {
	return (bytes/10)*7 + (bytes%10)*7/10
}
