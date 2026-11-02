package congestioncontrol

import (
	"testing"

	"load-balancer/model"
)

// The base RFC guard deliberately leaves a lost first data byte to the RTO.
func TestNewRenoInitialRecoveryGuard(t *testing.T) {
	policy := newRenoTestPolicy()
	initial := policy.State()
	response := policy.OnLoss(Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 1, NextSendSeq: 8193,
		FlightBytes: 8192, PendingSegments: 8,
	})
	if !response.SuppressFastRetransmit || policy.State() != initial || policy.activeRecovery {
		t.Fatalf("first-byte loss bypassed the initial recovery guard: %+v, %+v", response, policy)
	}

	// Acknowledging one data byte is enough to permit a new recovery episode.
	response = policy.OnLoss(Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 2, NextSendSeq: 8194,
		FlightBytes: 8192, PendingSegments: 8,
	})
	if response.SuppressFastRetransmit || !policy.activeRecovery ||
		policy.recoveryEndSeq != 8194 || policy.congestionWindowBytes != 7168 ||
		policy.slowStartThresholdBytes != 4096 || policy.State().Phase != "fastRecovery" {
		t.Fatalf("new loss did not enter recovery at 7 MSS: %+v, %+v", response, policy)
	}
}

// A 2-MSS partial ACK deflates 7 MSS to 6 MSS and retries the next hole.
func TestNewRenoPartialACKsDeflateAndOnlyFirstRestartsTimer(t *testing.T) {
	policy := newRenoTestPolicy()
	policy.OnLoss(newRenoTestLoss()) // ssthresh=4 MSS, cwnd=7 MSS, boundary=9217.
	first := policy.OnACK(ACK{CumulativeACK: 3073, NewBytes: 2048, FlightBytes: 6144})
	if !first.RetransmitOldest || first.PreserveRetransmitTimer ||
		!policy.activeRecovery || policy.congestionWindowBytes != 6144 {
		t.Fatalf("first partial ACK did not retry at 6 MSS with a fresh timer: %+v, %+v", first, policy)
	}
	second := policy.OnACK(ACK{CumulativeACK: 4097, NewBytes: 1024, FlightBytes: 5120})
	if !second.RetransmitOldest || !second.PreserveRetransmitTimer ||
		!policy.activeRecovery || policy.congestionWindowBytes != 6144 || policy.recoveryEndSeq != 9217 {
		t.Fatalf("later partial ACK changed the timer or recovery boundary: %+v, %+v", second, policy)
	}

	// A one-byte partial ACK removes one byte without adding back an MSS.
	policy.OnACK(ACK{CumulativeACK: 4098, NewBytes: 1})
	if policy.congestionWindowBytes != 6143 {
		t.Fatalf("one-byte ACK produced cwnd=%d, want 6143", policy.congestionWindowBytes)
	}

	// A separate flight ACKs 8191 of 8192 bytes at once and keeps the one-MSS floor.
	large := newRenoTestPolicy()
	large.OnLoss(newRenoTestLoss())
	large.OnACK(ACK{CumulativeACK: 9216, NewBytes: 8191, FlightBytes: 1})
	if !large.activeRecovery || large.congestionWindowBytes != 1024 {
		t.Fatalf("large partial ACK did not retain recovery at one MSS: %+v", large)
	}
}

// Finishing recovery with 0 or 2 MSS still outstanding leaves cwnd at 2 or 3 MSS.
func TestNewRenoFullACKDeflationUsesRemainingFlight(t *testing.T) {
	for _, test := range []struct {
		flight uint64
		window uint64
	}{
		{0, 2048},
		{1024, 2048},
		{2048, 3072},
		{4096, 4096},
	} {
		policy := newRenoTestPolicy()
		policy.OnLoss(newRenoTestLoss())
		response := policy.OnACK(ACK{CumulativeACK: 9217, NewBytes: 8192, FlightBytes: test.flight})
		if response != (ACKResponse{}) || policy.activeRecovery ||
			policy.congestionWindowBytes != test.window || policy.recoveryEndSeq != 9217 {
			t.Fatalf("full ACK with flight=%d: response=%+v, policy=%+v", test.flight, response, policy)
		}
		// The exit ACK must not also apply slow start or congestion avoidance.
		if policy.avoidanceAcknowledgedBytes != 0 {
			t.Fatal("full ACK also grew the congestion window")
		}
	}
}

// Eight outstanding segments allow five extra inflations and one loss reduction.
func TestNewRenoBoundsInflationAndRejectsSameFlight(t *testing.T) {
	policy := newRenoTestPolicy()
	policy.OnLoss(newRenoTestLoss())
	for count := 0; count < 20; count++ {
		policy.OnDuplicateACK()
	}
	if policy.congestionWindowBytes != 12288 || policy.remainingInflations != 0 {
		t.Fatalf("eight outstanding segments allowed more than five extra inflations: %+v", policy)
	}
	response := policy.OnLoss(newRenoTestLoss())
	if !response.SuppressFastRetransmit || policy.congestionWindowBytes != 12288 {
		t.Fatal("active recovery accepted another fast retransmit")
	}
	policy.OnACK(ACK{CumulativeACK: 9217, NewBytes: 8192})
	window := policy.State()
	response = policy.OnLoss(Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 9217, NextSendSeq: 13313,
		FlightBytes: 4096, PendingSegments: 4,
	})
	if !response.SuppressFastRetransmit || policy.State() != window {
		t.Fatal("ACK at the old boundary reopened recovery")
	}
	response = policy.OnLoss(Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 10241, NextSendSeq: 13313,
		FlightBytes: 3072, PendingSegments: 3,
	})
	if response.SuppressFastRetransmit || !policy.activeRecovery ||
		policy.slowStartThresholdBytes != 2048 || policy.congestionWindowBytes != 5120 {
		t.Fatalf("new flight did not use the two-MSS threshold minimum: %+v", policy)
	}
}

// Repeated RTOs keep the first 5-MSS threshold and protect the timed-out flight.
func TestNewRenoTimeoutRetainsThresholdAndRecoveryGuard(t *testing.T) {
	policy := newRenoTestPolicy()
	policy.OnLoss(newRenoTestLoss())
	policy.OnACK(ACK{CumulativeACK: 3073, NewBytes: 2048})
	policy.OnLoss(Loss{Kind: TimeoutLoss, FlightBytes: 10240, NextSendSeq: 13313})
	policy.OnLoss(Loss{Kind: TimeoutLoss, FlightBytes: 2048, NextSendSeq: 13313, AlreadyTimedOut: true})
	if policy.activeRecovery || policy.partialACKSeen || policy.remainingInflations != 0 ||
		policy.congestionWindowBytes != 1024 || policy.slowStartThresholdBytes != 5120 ||
		policy.recoveryEndSeq != 13313 {
		t.Fatalf("timeout did not clear recovery while retaining the first reduction: %+v", policy)
	}
	response := policy.OnLoss(Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 13313, NextSendSeq: 17409,
		FlightBytes: 4096, PendingSegments: 4,
	})
	if !response.SuppressFastRetransmit || policy.congestionWindowBytes != 1024 ||
		policy.slowStartThresholdBytes != 5120 {
		t.Fatal("duplicate ACKs from the timed-out flight caused a second loss response")
	}
	policy.OnACK(ACK{CumulativeACK: 14337, NewBytes: 1024})
	if policy.congestionWindowBytes != 2048 {
		t.Fatal("ACK after timeout did not resume classic slow start")
	}
}

func newRenoTestPolicy() *newRenoPolicy {
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionNewReno
	config.InitialWindowSegments = 8
	config.InitialSlowStartThresholdBytes = 16384
	return New(config).(*newRenoPolicy)
}

func newRenoTestLoss() Loss {
	return Loss{
		Kind: FastRetransmitLoss, CumulativeACK: 1025, NextSendSeq: 9217,
		FlightBytes: 8192, PendingSegments: 8,
	}
}
