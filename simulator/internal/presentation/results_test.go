package presentation

import (
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// A completed client leaves the routing table, but its measured RTT stays in the report.
func TestResultRetainsClientTimingAfterCompletionAndBackendRemoval(t *testing.T) {
	definition := projectionScenario()
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults = scenario.StreamConfig{
		Bytes:     64,
		PacketGap: time.Millisecond,
	}
	world := projectionWorld(t, definition)

	// The handshake has finished; the deliberate pause keeps payload unsent.
	advanceProjection(t, world, time.Millisecond)
	live := BuildResult(world, 0)
	if len(live.State.Servers) != 1 || len(live.State.Servers[0].Clients) != 1 {
		t.Fatal("expected one currently routed client")
	}
	if len(live.ClientTimings) != 1 {
		t.Fatal("expected one created client's timing")
	}
	handshakeTiming := live.ClientTimings[0].RTO
	if handshakeTiming.Samples != 1 || handshakeTiming.LatestRTTSeconds <= 0 {
		t.Fatalf("handshake timing = %+v", handshakeTiming)
	}

	// Finish the transfer and remove its backend to eliminate every current route.
	advanceProjection(t, world, 10*time.Millisecond)
	if err := world.RemoveBackend("one"); err != nil {
		t.Fatal(err)
	}
	finished := BuildResult(world, 0)
	if finished.CompletedStreams != 1 || len(finished.State.Servers) != 0 {
		t.Fatal("expected a completed transfer without a remaining backend")
	}
	if len(finished.ClientTimings) != 1 {
		t.Fatal("completed client's timing disappeared")
	}

	timing := finished.ClientTimings[0]
	if timing.ClientID != "client/clients/0" || timing.RTO.Samples != 2 {
		t.Fatalf("retained client timing = %+v", timing)
	}
	if timing.RTO.LatestRTTSeconds <= 0 || timing.RTO.SmoothedRTTSeconds <= 0 ||
		timing.RTO.RTTVariationSeconds <= 0 || timing.RTO.BaseRTOSeconds != 0.2 ||
		timing.RTO.CurrentRTOSeconds != 0.2 {
		t.Fatalf("retained RTT estimates = %+v", timing.RTO)
	}
}

// Reports show resolved defaults and explicit settings, including disabled cleanup.
func TestResultReportsEffectiveTimerConfiguration(t *testing.T) {
	fixedMode := "fixed"
	backoff := false
	initial := 250 * time.Millisecond
	minimum := 100 * time.Millisecond
	maximum := 2 * time.Second
	disabledIdle := time.Duration(0)
	halfOpenIdle := 750 * time.Millisecond
	establishedIdle := 3 * time.Second
	receiveBuffer := 4096

	tests := []struct {
		name        string
		tcp         scenario.TCPConfig
		halfOpen    *time.Duration
		established *time.Duration
		wantTCP     contract.TCPConfig
		wantLB      contract.LBIdleTimeoutConfig
	}{
		{
			name: "omitted defaults",
			wantTCP: contract.TCPConfig{
				ReceiveBufferBytes: 65535,
				RTO: contract.RTOConfig{
					Mode:                  "adaptive",
					ExponentialBackoff:    true,
					InitialTimeoutSeconds: 1,
					MinimumTimeoutSeconds: 0.2,
					MaximumTimeoutSeconds: 60,
				},
				ClientIdleTimeoutSeconds: 20,
			},
			wantLB: contract.LBIdleTimeoutConfig{
				HalfOpenIdleTimeoutSeconds:    5,
				EstablishedIdleTimeoutSeconds: 50,
			},
		},
		{
			name: "explicit fixed profile",
			tcp: scenario.TCPConfig{
				ReceiveBufferBytes: &receiveBuffer,
				RTO: scenario.RTOConfig{
					Mode:               &fixedMode,
					ExponentialBackoff: &backoff,
					InitialTimeout:     &initial,
					MinimumTimeout:     &minimum,
					MaximumTimeout:     &maximum,
				},
				ClientIdleTimeout: &disabledIdle,
			},
			halfOpen:    &halfOpenIdle,
			established: &establishedIdle,
			wantTCP: contract.TCPConfig{
				ReceiveBufferBytes: receiveBuffer,
				RTO: contract.RTOConfig{
					Mode:                  "fixed",
					ExponentialBackoff:    false,
					InitialTimeoutSeconds: 0.25,
					MinimumTimeoutSeconds: 0.1,
					MaximumTimeoutSeconds: 2,
				},
				ClientIdleTimeoutSeconds: 0,
			},
			wantLB: contract.LBIdleTimeoutConfig{
				HalfOpenIdleTimeoutSeconds:    0.75,
				EstablishedIdleTimeoutSeconds: 3,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := projectionScenario()
			definition.TCP = test.tcp
			definition.Resources.LoadBalancer.HalfOpenIdleTimeout = test.halfOpen
			definition.Resources.LoadBalancer.EstablishedIdleTimeout = test.established
			world := projectionWorld(t, definition)

			// No advancement is needed: effective settings exist before clients start.
			result := BuildResult(world, 0)
			if result.TCP != test.wantTCP {
				t.Fatalf("TCP configuration = %+v, want %+v", result.TCP, test.wantTCP)
			}
			if result.LBIdleTimeouts != test.wantLB {
				t.Fatalf("LB idle limits = %+v, want %+v", result.LBIdleTimeouts, test.wantLB)
			}
		})
	}
}
