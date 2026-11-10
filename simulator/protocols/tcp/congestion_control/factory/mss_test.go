package factory

import (
	"fmt"
	"testing"

	"load-balancer/model"
	"load-balancer/protocols/tcp/congestion_control"
)

// The same four-segment window is 5840 bytes at MSS=1460 and 35840 at MSS=8960.
// Every algorithm must use that selected MSS for growth, timeouts and ECN floors.
func TestConfiguredMSSControlsWindowBytes(t *testing.T) {
	for _, mode := range []model.CongestionMode{
		model.CongestionReno,
		model.CongestionNewReno,
		model.CongestionTahoe,
		model.CongestionCubic,
	} {
		for _, mssBytes := range []int{536, 1024, 1452, 1460, 8960} {
			name := fmt.Sprintf("%s/mss-%d", mode, mssBytes)
			t.Run(name, func(t *testing.T) {
				config := model.DefaultCongestionConfig()
				config.Mode = mode
				policy := NewWithMSS(config, mssBytes)
				segmentBytes := uint64(mssBytes)

				if got := policy.State().WindowBytes; got != 4*segmentBytes {
					t.Fatalf("initial window=%d, want four selected segments", got)
				}

				// A fresh one-segment ACK adds one selected segment during slow start.
				policy.OnACK(congestioncontrol.ACK{NewBytes: segmentBytes})
				if got := policy.State().WindowBytes; got != 5*segmentBytes {
					t.Fatalf("slow-start window=%d, want five selected segments", got)
				}

				// A timeout leaves one MSS; even a tiny flight keeps ssthresh at two MSS.
				policy.OnLoss(congestioncontrol.Loss{
					Kind:        congestioncontrol.TimeoutLoss,
					FlightBytes: segmentBytes,
				})
				state := policy.State()
				if state.WindowBytes != segmentBytes || state.ThresholdBytes != 2*segmentBytes {
					t.Fatalf("timeout used a different MSS: %+v", state)
				}

				// Repeated CE feedback cannot reduce the window below one selected MSS.
				policy.OnECN(congestioncontrol.ECN{FlightBytes: segmentBytes})
				state = policy.State()
				if state.WindowBytes != segmentBytes || state.ThresholdBytes != 2*segmentBytes {
					t.Fatalf("ECN used a different MSS floor: %+v", state)
				}
			})
		}
	}
}

// A one-segment ceiling means 1460 bytes here, even after an ACK tries to grow it.
func TestConfiguredMSSControlsWindowCeiling(t *testing.T) {
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
			policy := NewWithMSS(config, 1460)

			policy.OnACK(congestioncontrol.ACK{NewBytes: 1460})
			if got := policy.State().WindowBytes; got != 1460 {
				t.Fatalf("one-segment ceiling=%d, want 1460", got)
			}
		})
	}
}

// A jumbo-sized MSS also raises the smallest valid slow-start threshold.
// The public constructor rejects incompatible settings before traffic starts.
func TestConfiguredMSSValidatesThreshold(t *testing.T) {
	config := model.DefaultCongestionConfig()
	config.InitialSlowStartThresholdBytes = 2*8960 - 1

	defer func() {
		if recover() == nil {
			t.Fatal("accepted a threshold smaller than two selected segments")
		}
	}()
	NewWithMSS(config, 8960)
}

// The largest supported payload is valid with a matching two-segment threshold.
func TestMaximumMSSHasConsistentWindow(t *testing.T) {
	config := model.DefaultCongestionConfig()
	config.InitialSlowStartThresholdBytes = 2 * model.MaxMSSBytes
	policy := NewWithMSS(config, model.MaxMSSBytes)

	if got := policy.State().WindowBytes; got != 4*model.MaxMSSBytes {
		t.Fatalf("maximum MSS did not reach window configuration: got %d", got)
	}
}
