package simulation

import (
	"context"
	"fmt"
	"testing"

	"load-balancer/model"
)

// Every MTU-inspired preset sends the same 6000 bytes through the complete LB path.
// The window changes with MSS; the workload's two 3000-byte streams do not.
func TestWorldUsesManualMSS(t *testing.T) {
	for _, mssBytes := range []int{536, 1024, 1452, 1460, 8960} {
		t.Run(fmt.Sprint(mssBytes), func(t *testing.T) {
			definition := baseScenario()
			definition.TCP.MSSBytes = &mssBytes
			definition.Workload.Defaults.Bytes = 3000
			windowSegments := 1
			definition.TCP.CongestionControl.MaximumWindowSegments = &windowSegments
			world := New(normalizedRuntimeScenario(t, definition))

			if err := world.Advance(context.Background(), world.EndTime()); err != nil {
				t.Fatal(err)
			}
			info := world.Info()
			if info.MSSBytes != mssBytes || info.CompletedStreams != 2 {
				t.Fatalf("MSS=%d completed=%d, want MSS=%d and two streams",
					info.MSSBytes, info.CompletedStreams, mssBytes)
			}
			if info.Payload.AcknowledgedBytes != 6000 || info.Payload.ReceivedBytes != 6000 {
				t.Fatalf("changing packet size changed the payload: %+v", info.Payload)
			}

			// With a one-MSS ceiling, ACKs cannot grow cwnd beyond the chosen MSS.
			world.VisitClients(func(client ClientObservation) {
				window := client.State.Congestion.CongestionWindowBytes
				if window != uint64(mssBytes) {
					t.Fatalf("cwnd=%d, want one %d-byte MSS", window, mssBytes)
				}
				if client.State.Transfer.Status != model.TransferSucceeded {
					t.Fatalf("transfer did not complete: %+v", client.State.Transfer)
				}
			})
		})
	}
}

// Both valid endpoints of the range need their matching two-MSS threshold.
// Applying either setting against the old MSS first would reject one of these.
func TestWorldAppliesMSSAndCongestionTogether(t *testing.T) {
	for _, mssBytes := range []int{536, model.MaxMSSBytes} {
		t.Run(fmt.Sprint(mssBytes), func(t *testing.T) {
			definition := baseScenario()
			definition.TCP.MSSBytes = &mssBytes
			thresholdBytes := uint64(2 * mssBytes)
			definition.TCP.CongestionControl.InitialSlowStartThresholdBytes = &thresholdBytes
			world := New(normalizedRuntimeScenario(t, definition))

			if err := world.Advance(context.Background(), world.EndTime()); err != nil {
				t.Fatal(err)
			}
			if got := world.Info().CompletedStreams; got != 2 {
				t.Fatalf("MSS=%d completed %d streams, want two", mssBytes, got)
			}
		})
	}
}
