package factory

import (
	"testing"
	"time"

	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// A one-MSS ceiling stays at one MSS even when many ACKs would normally grow cwnd.
// The receive window remains large: this tests a real congestion-window limit.
func TestMaximumWindowKeepsOneSegment(t *testing.T) {
	for _, mode := range []model.CongestionMode{
		model.CongestionReno,
		model.CongestionNewReno,
		model.CongestionTahoe,
		model.CongestionCubic,
	} {
		t.Run(string(mode), func(t *testing.T) {
			config := model.DefaultCongestionConfig()
			config.Mode = mode
			config.MaximumWindowSegments = 1
			policy := New(config)

			// The usual initial four MSS are also clamped to the configured ceiling.
			if state := policy.State(); state.WindowBytes != model.SEGMENT_SIZE {
				t.Fatalf("initial cwnd=%d, want one MSS", state.WindowBytes)
			}
			for index := 1; index <= 20; index++ {
				now := time.Duration(index) * time.Millisecond
				policy.OnSenderState(congestioncontrol.SenderState{
					At:                 now,
					HasData:            true,
					ReceiveWindowBytes: 65535,
				})
				policy.OnACK(congestioncontrol.ACK{
					At:            now,
					NewBytes:      model.SEGMENT_SIZE,
					CumulativeACK: uint64(index*model.SEGMENT_SIZE + 1),
					SmoothedRTT:   time.Millisecond,
				})
				if state := policy.State(); state.WindowBytes != model.SEGMENT_SIZE {
					t.Fatalf("ACK %d grew cwnd past one MSS: %+v", index, state)
				}
			}
			policy.OnECN(congestioncontrol.ECN{FlightBytes: model.SEGMENT_SIZE})
			policy.OnLoss(congestioncontrol.Loss{Kind: congestioncontrol.TimeoutLoss, FlightBytes: model.SEGMENT_SIZE})
			policy.OnIdle()
			if state := policy.State(); state.WindowBytes != model.SEGMENT_SIZE {
				t.Fatalf("loss/idle changed the one-MSS window: %+v", state)
			}
		})
	}
}

// With four MSS in flight, fast recovery normally inflates the window to five.
// The configured ceiling must also apply to that temporary inflation.
func TestMaximumWindowBoundsRecoveryInflation(t *testing.T) {
	for _, mode := range []model.CongestionMode{
		model.CongestionReno,
		model.CongestionNewReno,
		model.CongestionCubic,
	} {
		t.Run(string(mode), func(t *testing.T) {
			config := model.DefaultCongestionConfig()
			config.Mode = mode
			config.MaximumWindowSegments = 4
			policy := New(config)
			policy.OnLoss(congestioncontrol.Loss{
				Kind:            congestioncontrol.FastRetransmitLoss,
				FlightBytes:     4 * model.SEGMENT_SIZE,
				CumulativeACK:   2,
				NextSendSeq:     4097,
				PendingSegments: 4,
			})
			policy.OnDuplicateACK()
			if state := policy.State(); state.WindowBytes > 4*model.SEGMENT_SIZE {
				t.Fatalf("recovery inflated cwnd beyond its ceiling: %+v", state)
			}
		})
	}
}
