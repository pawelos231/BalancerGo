package cubic

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/protocols/tcp/congestion_control"
)

// CUBIC remembers the pre-ECN window; fast convergence runs once per actual reduction.
func TestCubicECNRemembersMaximumAndRestartsEpoch(t *testing.T) {
	policy := cubicTestPolicy(80)
	policy.previousMaximumSegments = 100
	policy.fastConvergence = true
	policy.epochStarted = true
	policy.OnECN(congestioncontrol.ECN{At: time.Second, FlightBytes: 80 * model.SEGMENT_SIZE})
	if policy.previousMaximumSegments != 68 || policy.priorWindowSegments != 80 ||
		policy.CongestionWindowBytes != 56*model.SEGMENT_SIZE || policy.epochStarted || policy.resetMaximumAtEpoch {
		t.Fatalf("ECN lost CUBIC's maximum or epoch bookkeeping: %+v", policy)
	}

	policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, FlightBytes: 40 * model.SEGMENT_SIZE,
		AlreadyReduced: true, SACKRecovery: true,
	})
	if policy.previousMaximumSegments != 68 || policy.priorWindowSegments != 80 {
		t.Fatalf("same-flight loss applied fast convergence again: %+v", policy)
	}
}
