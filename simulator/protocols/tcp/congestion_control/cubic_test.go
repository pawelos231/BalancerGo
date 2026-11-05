package congestioncontrol

import (
	"math"
	"testing"
	"time"

	"load-balancer/model"
)

// Virtual time zero is a real epoch: a second ACK must continue, not restart, its growth.
func TestCubicEpochAtZeroAndFractionalACKGrowth(t *testing.T) {
	policy := cubicTestPolicy(10)
	policy.OnACK(cubicTestACK(0, 1024))
	policy.OnACK(cubicTestACK(100*time.Millisecond, 1024))
	if !policy.epochStarted || policy.epochActiveAt != 0 ||
		policy.previousMaximumSegments != 10 || policy.windowSegments < 10.19 {
		t.Fatalf("second ACK restarted or stalled the zero-time epoch: %+v", policy)
	}

	// Each one-byte ACK adds less than one window byte; retaining fractions prevents a stall.
	fractional := cubicTestPolicy(4)
	for count := 0; count < 8; count++ {
		fractional.OnACK(cubicTestACK(0, 1))
	}
	if fractional.congestionWindowBytes <= 4096 {
		t.Fatal("sub-byte congestion growth was discarded")
	}
}

// RFC 9438 uses the actual epoch window in K, even when flight was smaller than cwnd.
func TestCubicEpochUsesActualReducedWindow(t *testing.T) {
	policy := cubicTestPolicy(100)
	policy.OnLoss(Loss{Kind: FastRetransmitLoss, FlightBytes: 80 * 1024})
	policy.OnACK(cubicTestACK(0, 1024)) // Exit recovery at 56 MSS.
	policy.OnACK(cubicTestACK(0, 1024)) // Equality with ssthresh grows slow start to 57 MSS.
	policy.OnACK(cubicTestACK(0, 1024)) // Begin congestion avoidance at that actual window.

	// W_max=100, cwnd_epoch=57, C=0.4 gives K=cuberoot(107.5).
	wantK := math.Cbrt(107.5)
	if math.Abs(policy.returnToMaximumSecs-wantK) > 1e-12 {
		t.Fatalf("K=%f did not use the actual 57-MSS epoch window", policy.returnToMaximumSecs)
	}
}

// Numeric examples distinguish both sides of the cubic plateau and the target's upper bound.
func TestCubicCurveGrowthAndTargetClamp(t *testing.T) {
	tests := []struct {
		name    string
		window  float64
		maximum float64
		k       float64
		elapsed time.Duration
		want    float64
	}{
		{"concave", 74.4, 100, 4, time.Second, 74.6129623655914},
		{"convex", 100, 100, 0, time.Second, 100.005324},
		{"target capped at 1.5 windows", 10, 10, 0, 100 * time.Second, 10.5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := cubicTestPolicy(10)
			policy.windowSegments = test.window
			policy.storeFractionalWindow()
			policy.previousMaximumSegments = test.maximum
			policy.priorWindowSegments = test.maximum
			policy.epochStarted = true
			policy.returnToMaximumSecs = test.k
			policy.renoWindowSegments = test.window
			policy.renoAlpha = cubicAlpha

			policy.OnACK(cubicTestACK(test.elapsed, 1024))
			if math.Abs(policy.windowSegments-test.want) > 1e-11 {
				t.Fatalf("window=%f MSS, want %f", policy.windowSegments, test.want)
			}
		})
	}
}

// Reno friendliness changes alpha at cwnd_prior, not at a fast-convergence-adjusted W_max.
func TestCubicRenoFriendlyAlphaReachesPriorWindow(t *testing.T) {
	policy := cubicTestPolicy(7)
	policy.previousMaximumSegments = 8
	policy.priorWindowSegments = 10
	policy.resetMaximumAtEpoch = false
	policy.startEpoch(0)
	policy.renoWindowSegments = 9.99
	policy.OnACK(cubicTestACK(0, 1024))
	if policy.renoAlpha != 1 || policy.renoWindowSegments <= 10 {
		t.Fatalf("crossing cwnd_prior did not select alpha=1: %+v", policy)
	}
}

// A second loss below the old maximum can reserve more room for competing flows.
func TestCubicFastConvergenceIsOptional(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		policy := cubicTestPolicy(100)
		policy.fastConvergence = enabled
		policy.previousMaximumSegments = 100
		policy.setWindowBytes(80 * 1024)
		policy.OnLoss(Loss{Kind: FastRetransmitLoss, FlightBytes: 80 * 1024})
		wantMaximum := 80.0
		if enabled {
			wantMaximum = 68 // 80 * (1 + 0.7) / 2.
		}
		if policy.previousMaximumSegments != wantMaximum || policy.priorWindowSegments != 80 ||
			policy.slowStartThresholdBytes != 56*1024 {
			t.Fatalf("fast convergence=%v: %+v", enabled, policy)
		}
	}
}

// Repeated RTOs preserve the first reduction; the next CA stage uses K=0 and a new maximum.
func TestCubicTimeoutResetsNextEpochOnlyOnce(t *testing.T) {
	policy := cubicTestPolicy(100)
	policy.OnLoss(Loss{Kind: TimeoutLoss, FlightBytes: 100 * 1024})
	policy.OnLoss(Loss{
		Kind: TimeoutLoss, FlightBytes: 60 * 1024, AlreadyTimedOut: true,
	})
	if policy.slowStartThresholdBytes != 70*1024 || policy.priorWindowSegments != 100 ||
		policy.congestionWindowBytes != 1024 {
		t.Fatalf("a repeated timeout reduced the threshold again: %+v", policy)
	}
	policy.setWindowBytes(71 * 1024) // Slow start has just crossed the retained threshold.
	policy.OnACK(cubicTestACK(2*time.Second, 1024))
	if policy.returnToMaximumSecs != 0 || policy.previousMaximumSegments != 71 {
		t.Fatalf("post-timeout epoch reused the old maximum: %+v", policy)
	}
}

// A fresh fast loss supersedes the post-RTO reset, even if it happens during slow start.
func TestCubicFreshFastLossReplacesPendingTimeoutEpoch(t *testing.T) {
	policy := cubicTestPolicy(100)
	policy.OnLoss(Loss{Kind: TimeoutLoss, FlightBytes: 100 * 1024})
	policy.setWindowBytes(10 * 1024)
	policy.OnLoss(Loss{Kind: FastRetransmitLoss, FlightBytes: 10 * 1024})
	policy.OnACK(cubicTestACK(0, 1024))
	policy.OnACK(cubicTestACK(0, 1024)) // Equality with the threshold still uses slow start.
	policy.OnACK(cubicTestACK(0, 1024))
	if policy.previousMaximumSegments != 8.5 || policy.returnToMaximumSecs == 0 {
		t.Fatalf("a stale timeout marker replaced the newer loss epoch: %+v", policy)
	}
}

// Very large configured windows and a zero measured RTT must keep valid integer byte limits.
func TestCubicWindowConversionStaysWithinByteLimit(t *testing.T) {
	policy := cubicTestPolicy(100)
	policy.OnACK(ACK{NewBytes: 1024})
	policy.windowSegments = float64(model.MaxCongestionWindowBytes)/1024 + 1000
	policy.storeFractionalWindow()
	if policy.congestionWindowBytes != model.MaxCongestionWindowBytes {
		t.Fatalf("large floating-point window wrapped to %d", policy.congestionWindowBytes)
	}
	threshold := cubicLossThreshold(model.MaxCongestionWindowBytes, model.DefaultMSSBytes)
	if threshold > model.MaxCongestionWindowBytes {
		t.Fatal("loss threshold overflowed")
	}
}

func cubicTestPolicy(initialSegments int) *cubicPolicy {
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = initialSegments
	config.InitialSlowStartThresholdBytes = 2 * model.SEGMENT_SIZE
	return newCubicPolicy(config, model.DefaultMSSBytes)
}

func cubicTestACK(at time.Duration, bytes uint64) ACK {
	return ACK{At: at, NewBytes: bytes, SmoothedRTT: 100 * time.Millisecond}
}
