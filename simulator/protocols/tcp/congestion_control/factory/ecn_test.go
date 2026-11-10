package factory

import (
	"testing"

	"load-balancer/model"
	"load-balancer/protocols/tcp/congestion_control"
)

// Marking eight MSS leaves four MSS (CUBIC: 5.6 MSS), with no missing-packet state.
// ECE ACKs still confirm delivery, but do not immediately undo the reduction.
func TestECNReducesWithoutFastRecoveryOrACKGrowth(t *testing.T) {
	for _, mode := range ecnTestModes() {
		t.Run(string(mode), func(t *testing.T) {
			policy := newECNTestPolicy(mode, 8)
			policy.OnECN(congestioncontrol.ECN{FlightBytes: 8192})
			want := uint64(4096)
			if mode == model.CongestionCubic {
				want = 5734
			}
			state := policy.State()
			if state.WindowBytes != want || state.ThresholdBytes != want || state.Phase == "fastRecovery" {
				t.Fatalf("ECN entered loss recovery or used the wrong reduction: %+v", state)
			}

			response := policy.OnACK(congestioncontrol.ACK{NewBytes: 8192, CumulativeACK: 8193, ECN: true})
			policy.OnDuplicateACK()
			if response != (congestioncontrol.ACKResponse{}) || policy.State() != state {
				t.Fatalf("ECE ACK requested a retry or grew cwnd: response=%+v, state=%+v", response, policy.State())
			}
		})
	}
}

// ECN's one-reduction rule must not hide a real hole from the SACK repair path.
func TestLossAfterECNRepairsWithoutReducingThresholdAgain(t *testing.T) {
	for _, mode := range ecnTestModes() {
		t.Run(string(mode), func(t *testing.T) {
			policy := newECNTestPolicy(mode, 8)
			policy.OnECN(congestioncontrol.ECN{FlightBytes: 8192})
			threshold := policy.State().ThresholdBytes
			response := policy.OnLoss(congestioncontrol.Loss{
				Kind: congestioncontrol.FastRetransmitLoss, FlightBytes: 4096,
				CumulativeACK: 1, NextSendSeq: 8193, PendingSegments: 4,
				SACKRecovery: true, AlreadyReduced: true,
			})
			state := policy.State()
			wantRecovery := mode != model.CongestionTahoe
			if response.SuppressFastRetransmit || state.ThresholdBytes != threshold ||
				state.WindowBytes != threshold || (state.Phase == "fastRecovery") != wantRecovery {
				t.Fatalf("ECN prevented repair or loss reduced the same flight again: response=%+v, state=%+v", response, state)
			}

			// A lost retransmission still invokes the ordinary one-MSS timeout response.
			policy.OnLoss(congestioncontrol.Loss{Kind: congestioncontrol.TimeoutLoss, FlightBytes: 4096, NextSendSeq: 8193})
			if state := policy.State(); state.WindowBytes != model.SEGMENT_SIZE || state.Phase != "slowStart" {
				t.Fatalf("a real RTO after ECN did not restart slow start: %+v", state)
			}
		})
	}
}

// The two-MSS threshold floor must not keep the actual ECN window above one MSS.
// TCP owns the additional retransmission-timer backoff at this minimum.
func TestPersistentECNReachesOneMSS(t *testing.T) {
	for _, mode := range ecnTestModes() {
		t.Run(string(mode), func(t *testing.T) {
			policy := newECNTestPolicy(mode, 2)
			for count := 0; count < 4; count++ {
				policy.OnECN(congestioncontrol.ECN{FlightBytes: policy.State().WindowBytes})
			}
			if state := policy.State(); state.WindowBytes != model.SEGMENT_SIZE || state.ThresholdBytes != 2*model.SEGMENT_SIZE {
				t.Fatalf("persistent ECN failed to reach the one-MSS floor: %+v", state)
			}
		})
	}
}

// A receiver-limited flight can make ssthresh much smaller than the ECN window;
// at the one-MSS floor it is larger. Repair must retain the reduced base in both
// cases, with classic inflation removed again when recovery completes.
func TestLossAfterECNPreservesWindowWhenThresholdDiffers(t *testing.T) {
	for _, mode := range []model.CongestionMode{model.CongestionReno, model.CongestionNewReno, model.CongestionCubic} {
		for _, floor := range []bool{false, true} {
			for _, sack := range []bool{false, true} {
				policy := newECNTestPolicy(mode, 16)
				policy.OnECN(congestioncontrol.ECN{FlightBytes: 4 * model.SEGMENT_SIZE})
				if floor {
					for count := 0; count < 16; count++ {
						policy.OnECN(congestioncontrol.ECN{FlightBytes: policy.State().WindowBytes})
					}
				}
				base, threshold := policy.State().WindowBytes, policy.State().ThresholdBytes
				response := policy.OnLoss(congestioncontrol.Loss{Kind: congestioncontrol.FastRetransmitLoss, FlightBytes: 1024,
					CumulativeACK: 1, NextSendSeq: 100, PendingSegments: 4,
					AlreadyReduced: true, SACKRecovery: sack})
				want := base
				if !sack {
					want += 3 * model.SEGMENT_SIZE
				}
				if response.SuppressFastRetransmit || policy.State().WindowBytes != want || policy.State().ThresholdBytes != threshold {
					t.Fatalf("%s floor=%v SACK=%v: repair changed ECN base=%d threshold=%d: %+v", mode, floor, sack, base, threshold, policy.State())
				}
				policy.OnACK(congestioncontrol.ACK{CumulativeACK: 100, NewBytes: 1, FlightBytes: base,
					SACKRecovery: sack, EndSACKRecovery: sack, ECN: true})
				if policy.State().WindowBytes != base || policy.State().ThresholdBytes != threshold || policy.State().Phase == "fastRecovery" {
					t.Fatalf("%s floor=%v SACK=%v: recovery completion lost ECN base=%d: %+v", mode, floor, sack, base, policy.State())
				}
			}
		}
	}
}

func ecnTestModes() []model.CongestionMode {
	return []model.CongestionMode{
		model.CongestionReno, model.CongestionTahoe,
		model.CongestionNewReno, model.CongestionCubic,
	}
}

func newECNTestPolicy(mode model.CongestionMode, segments int) congestioncontrol.Policy {
	config := model.DefaultCongestionConfig()
	config.Mode = mode
	config.InitialWindowSegments = segments
	return New(config)
}
