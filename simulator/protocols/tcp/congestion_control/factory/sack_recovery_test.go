package factory

import (
	"testing"

	"load-balancer/model"
	"load-balancer/protocols/tcp/congestion_control"
)

// An eight-segment flight enters SACK recovery without classic Reno's extra three MSS.
// Repairing one hole keeps the reduced window until the original flight is ACKed.
func TestSACKRecoveryKeepsWindowUntilWholeFlightIsAcknowledged(t *testing.T) {
	for _, mode := range sackRecoveryTestModes() {
		t.Run(string(mode), func(t *testing.T) {
			policy := newSACKRecoveryTestPolicy(mode)
			response := policy.OnLoss(sackRecoveryTestLoss())
			threshold := uint64(4096)
			if mode == model.CongestionCubic {
				threshold = 5734 // floor(8192 * 0.7).
			}

			state := policy.State()
			if response.SuppressFastRetransmit || state.Phase != "fastRecovery" ||
				state.WindowBytes != threshold || state.ThresholdBytes != threshold {
				t.Fatalf("SACK entry did not retain the reduced window: response=%+v, state=%+v", response, state)
			}

			// A partial ACK repairs an early hole, while later missing data remains.
			partial := policy.OnACK(congestioncontrol.ACK{
				CumulativeACK: 2049,
				NewBytes:      2048,
				FlightBytes:   6144,
				SACKRecovery:  true,
			})
			if partial != (congestioncontrol.ACKResponse{}) || policy.State() != state {
				t.Fatalf(
					"partial SACK ACK changed recovery or requested another retry: response=%+v, state=%+v",
					partial, policy.State(),
				)
			}

			// TCP normally consumes duplicate ACKs itself; they cannot inflate this window.
			policy.OnDuplicateACK()
			if policy.State() != state {
				t.Fatalf("duplicate ACK inflated the SACK window: %+v", policy.State())
			}

			// One cumulative ACK covers the original eight segments and ends recovery.
			full := policy.OnACK(congestioncontrol.ACK{
				CumulativeACK:   8193,
				NewBytes:        6144,
				SACKRecovery:    true,
				EndSACKRecovery: true,
			})
			finished := policy.State()
			if full != (congestioncontrol.ACKResponse{}) || finished.Phase == "fastRecovery" ||
				finished.WindowBytes != threshold {
				t.Fatalf("full ACK failed to end recovery without extra growth: response=%+v, state=%+v", full, finished)
			}

			// Later ordinary ACKs resume the selected algorithm's normal growth.
			policy.OnACK(congestioncontrol.ACK{NewBytes: threshold, CumulativeACK: 8193 + threshold})
			if policy.State().WindowBytes != threshold+model.SEGMENT_SIZE {
				t.Fatalf("normal growth did not resume after SACK recovery: %+v", policy.State())
			}
		})
	}
}

// An RTO abandons SACK recovery and applies the ordinary one-MSS timeout restart.
func TestSACKRecoveryTimeoutRestartsSlowStart(t *testing.T) {
	for _, mode := range sackRecoveryTestModes() {
		t.Run(string(mode), func(t *testing.T) {
			policy := newSACKRecoveryTestPolicy(mode)
			policy.OnLoss(sackRecoveryTestLoss())
			policy.OnLoss(congestioncontrol.Loss{
				Kind:          congestioncontrol.TimeoutLoss,
				FlightBytes:   6144,
				NextSendSeq:   8193,
				CumulativeACK: 2049,
			})

			state := policy.State()
			if state.Phase != "slowStart" || state.WindowBytes != model.SEGMENT_SIZE {
				t.Fatalf("RTO retained SACK recovery instead of restarting: %+v", state)
			}

			// Once the retry is acknowledged, growth starts from one segment again.
			policy.OnACK(congestioncontrol.ACK{NewBytes: model.SEGMENT_SIZE, CumulativeACK: 3073})
			if policy.State().WindowBytes != 2*model.SEGMENT_SIZE {
				t.Fatalf("timeout recovery did not grow from one to two segments: %+v", policy.State())
			}
		})
	}
}

// These are the explicitly supported RFC 6675 combinations for the current profile.
func sackRecoveryTestModes() []model.CongestionMode {
	return []model.CongestionMode{
		model.CongestionReno,
		model.CongestionNewReno,
		model.CongestionCubic,
	}
}

// Start with eight MSS; SACK detection is allowed even when the first data byte was lost.
func newSACKRecoveryTestPolicy(mode model.CongestionMode) congestioncontrol.Policy {
	config := model.DefaultCongestionConfig()
	config.Mode = mode
	config.InitialWindowSegments = 8
	return New(config)
}

func sackRecoveryTestLoss() congestioncontrol.Loss {
	return congestioncontrol.Loss{
		Kind:            congestioncontrol.FastRetransmitLoss,
		FlightBytes:     8192,
		CumulativeACK:   1,
		NextSendSeq:     8193,
		PendingSegments: 8,
		SACKRecovery:    true,
	}
}
