package newreno

import (
	"testing"

	"load-balancer/model"
	"load-balancer/protocols/tcp/congestion_control"
)

// The base RFC guard deliberately leaves a lost first data byte to the RTO.
func TestNewRenoInitialRecoveryGuard(t *testing.T) {
	policy := newRenoTestPolicy()
	initial := policy.State()
	response := policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 1, NextSendSeq: 8193,
		FlightBytes: 8192, PendingSegments: 8,
	})
	if !response.SuppressFastRetransmit || policy.State() != initial {
		t.Fatalf("first-byte loss bypassed the initial recovery guard: response=%+v, state=%+v", response, policy.State())
	}

	// Acknowledging one data byte is enough to permit a new recovery episode.
	response = policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 2, NextSendSeq: 8194,
		FlightBytes: 8192, PendingSegments: 8,
	})
	state := policy.State()
	if response.SuppressFastRetransmit || state.Phase != "fastRecovery" ||
		state.WindowBytes != 7168 || state.ThresholdBytes != 4096 {
		t.Fatalf("new loss did not enter recovery at 7 MSS: response=%+v, state=%+v", response, state)
	}
}

// A 2-MSS partial ACK deflates 7 MSS to 6 MSS and retries the next hole.
func TestNewRenoPartialACKsDeflateAndOnlyFirstRestartsTimer(t *testing.T) {
	policy := newRenoTestPolicy()
	policy.OnLoss(newRenoTestLoss()) // ssthresh=4 MSS, cwnd=7 MSS, boundary=9217.
	first := policy.OnACK(congestioncontrol.ACK{CumulativeACK: 3073, NewBytes: 2048, FlightBytes: 6144})
	state := policy.State()
	if !first.RetransmitOldest || first.PreserveRetransmitTimer ||
		state.Phase != "fastRecovery" || state.WindowBytes != 6144 {
		t.Fatalf("first partial ACK did not retry at 6 MSS with a fresh timer: response=%+v, state=%+v", first, state)
	}
	second := policy.OnACK(congestioncontrol.ACK{CumulativeACK: 4097, NewBytes: 1024, FlightBytes: 5120})
	state = policy.State()
	if !second.RetransmitOldest || !second.PreserveRetransmitTimer ||
		state.Phase != "fastRecovery" || state.WindowBytes != 6144 {
		t.Fatalf("later partial ACK changed the timer or recovery window: response=%+v, state=%+v", second, state)
	}

	// A one-byte partial ACK removes one byte without adding back an MSS.
	policy.OnACK(congestioncontrol.ACK{CumulativeACK: 4098, NewBytes: 1})
	if state := policy.State(); state.WindowBytes != 6143 {
		t.Fatalf("one-byte ACK produced cwnd=%d, want 6143", state.WindowBytes)
	}

	// A separate flight ACKs 8191 of 8192 bytes at once and keeps the one-MSS floor.
	large := newRenoTestPolicy()
	large.OnLoss(newRenoTestLoss())
	large.OnACK(congestioncontrol.ACK{CumulativeACK: 9216, NewBytes: 8191, FlightBytes: 1})
	if state := large.State(); state.Phase != "fastRecovery" || state.WindowBytes != 1024 {
		t.Fatalf("large partial ACK did not retain recovery at one MSS: %+v", state)
	}
	response := large.OnACK(congestioncontrol.ACK{CumulativeACK: 9217, NewBytes: 1})
	if state := large.State(); response != (congestioncontrol.ACKResponse{}) || state.Phase == "fastRecovery" {
		t.Fatalf("partial ACK moved the recovery boundary: response=%+v, state=%+v", response, state)
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
		response := policy.OnACK(congestioncontrol.ACK{CumulativeACK: 9217, NewBytes: 8192, FlightBytes: test.flight})
		state := policy.State()
		if response != (congestioncontrol.ACKResponse{}) || state.Phase == "fastRecovery" || state.WindowBytes != test.window {
			t.Fatalf("full ACK with flight=%d: response=%+v, state=%+v", test.flight, response, state)
		}
		// The exit ACK must not also apply slow start or congestion avoidance.
		policy.OnACK(congestioncontrol.ACK{CumulativeACK: 10241, NewBytes: 1024})
		want := test.window
		if want < 4096 {
			want += 1024
		}
		if state := policy.State(); state.WindowBytes != want {
			t.Fatalf("full ACK contributed extra growth: flight=%d, state=%+v, want cwnd=%d", test.flight, state, want)
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
	if state := policy.State(); state.WindowBytes != 12288 {
		t.Fatalf("eight outstanding segments allowed more than five extra inflations: %+v", state)
	}
	response := policy.OnLoss(newRenoTestLoss())
	if !response.SuppressFastRetransmit || policy.State().WindowBytes != 12288 {
		t.Fatal("active recovery accepted another fast retransmit")
	}
	policy.OnACK(congestioncontrol.ACK{CumulativeACK: 9217, NewBytes: 8192})
	window := policy.State()
	response = policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 9217, NextSendSeq: 13313,
		FlightBytes: 4096, PendingSegments: 4,
	})
	if !response.SuppressFastRetransmit || policy.State() != window {
		t.Fatal("ACK at the old boundary reopened recovery")
	}
	response = policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 10241, NextSendSeq: 13313,
		FlightBytes: 3072, PendingSegments: 3,
	})
	state := policy.State()
	if response.SuppressFastRetransmit || state.Phase != "fastRecovery" ||
		state.ThresholdBytes != 2048 || state.WindowBytes != 5120 {
		t.Fatalf("new flight did not use the two-MSS threshold minimum: response=%+v, state=%+v", response, state)
	}
}

// Repeated RTOs keep the first 5-MSS threshold and protect the timed-out flight.
func TestNewRenoTimeoutRetainsThresholdAndRecoveryGuard(t *testing.T) {
	policy := newRenoTestPolicy()
	policy.OnLoss(newRenoTestLoss())
	policy.OnACK(congestioncontrol.ACK{CumulativeACK: 3073, NewBytes: 2048})
	policy.OnLoss(congestioncontrol.Loss{Kind: congestioncontrol.TimeoutLoss, FlightBytes: 10240, NextSendSeq: 13313})
	policy.OnLoss(congestioncontrol.Loss{Kind: congestioncontrol.TimeoutLoss, FlightBytes: 2048, NextSendSeq: 13313, AlreadyTimedOut: true})
	state := policy.State()
	if state.Phase != "slowStart" || state.WindowBytes != 1024 || state.ThresholdBytes != 5120 {
		t.Fatalf("timeout did not clear recovery while retaining the first reduction: %+v", state)
	}
	policy.OnDuplicateACK()
	response := policy.OnLoss(congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 13313, NextSendSeq: 17409,
		FlightBytes: 4096, PendingSegments: 4,
	})
	if !response.SuppressFastRetransmit || policy.State() != state {
		t.Fatal("duplicate ACKs from the timed-out flight caused a second loss response")
	}
	ackResponse := policy.OnACK(congestioncontrol.ACK{CumulativeACK: 14337, NewBytes: 1024})
	if state := policy.State(); ackResponse != (congestioncontrol.ACKResponse{}) || state.WindowBytes != 2048 {
		t.Fatalf("ACK after timeout did not resume classic slow start: response=%+v, state=%+v", ackResponse, state)
	}
}

func newRenoTestPolicy() congestioncontrol.Policy {
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionNewReno
	config.InitialWindowSegments = 8
	config.InitialSlowStartThresholdBytes = 16384
	return newNewRenoPolicy(config, model.DefaultMSSBytes)
}

func newRenoTestLoss() congestioncontrol.Loss {
	return congestioncontrol.Loss{
		Kind: congestioncontrol.FastRetransmitLoss, CumulativeACK: 1025, NextSendSeq: 9217,
		FlightBytes: 8192, PendingSegments: 8,
	}
}
