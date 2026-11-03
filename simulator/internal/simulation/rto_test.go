package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// A blackholed SYN can retry through 63 s when client/LB idle timers are disabled.
// Only the client's idle limit ends its attempt; LB cleanup leaves the sender alone.
func TestWorldRTOBlackholedSYNIdlePolicies(t *testing.T) {
	for _, test := range []struct {
		name            string
		clientIdle      time.Duration
		halfOpenIdle    time.Duration
		establishedIdle time.Duration
		until           time.Duration
		failure         model.TransferFailureReason
		elapsed         time.Duration
		retries         uint64
		currentRTO      time.Duration
	}{
		{
			name: "all idle timers disabled", until: 70 * time.Second,
			elapsed: 70 * time.Second, retries: 6, currentRTO: time.Minute,
		},
		{
			name: "client idle timeout", clientIdle: 750 * time.Millisecond,
			until: 40 * time.Second, failure: model.TransferReadTimeout,
			elapsed: 750 * time.Millisecond, currentRTO: time.Second,
		},
		{
			name: "LB half-open limit does not stop sender", halfOpenIdle: time.Second,
			until: 40 * time.Second, elapsed: 40 * time.Second,
			retries: 5, currentRTO: 32 * time.Second,
		},
		{
			name: "established timer ignores SYN", establishedIdle: time.Millisecond,
			until: 40 * time.Second, elapsed: 40 * time.Second,
			retries: 5, currentRTO: 32 * time.Second,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := scenarioRuntimeDefinition()
			input.Execution.Duration = test.until
			input.Resources.Backends = nil
			input.TCP.ClientIdleTimeout = &test.clientIdle
			input.Resources.LoadBalancer.HalfOpenIdleTimeout = &test.halfOpenIdle
			input.Resources.LoadBalancer.EstablishedIdleTimeout = &test.establishedIdle
			world := New(normalizedRuntimeScenario(t, input))
			t.Cleanup(world.Stop)

			runIngressUntil(t, world, test.until)
			observed := onlyRTOClient(t, world)
			transfer := observed.State.Transfer
			if observed.Active != (test.failure == "") || transfer.FailureReason != test.failure {
				t.Fatalf("idle policy outcome = %+v", observed)
			}
			if transfer.Elapsed != test.elapsed || transfer.SynRetransmissions != test.retries {
				t.Fatalf("idle policy timing = %+v, want elapsed=%s retries=%d",
					transfer, test.elapsed, test.retries)
			}
			if transfer.HandshakeCompleted || observed.State.RTO.Samples != 0 {
				t.Fatalf("blackholed SYN produced a handshake or RTT sample: %+v", observed.State)
			}
			if observed.State.RTO.CurrentRTO != test.currentRTO {
				t.Fatalf("RTO = %+v, want current timeout %s", observed.State.RTO, test.currentRTO)
			}
		})
	}
}

// An established connection ignores the 1 ms half-open limit while waiting to send.
// The LB's 2 s idle limit expires its route at 5 s, leaving both endpoints alive.
func TestWorldRTOEstablishedIdlePolicy(t *testing.T) {
	for _, establishedIdle := range []time.Duration{0, 2 * time.Second} {
		t.Run(establishedIdle.String(), func(t *testing.T) {
			input := scenarioRuntimeDefinition()
			input.Execution.Duration = 70 * time.Second
			input.Resources.Backends = input.Resources.Backends[:1]
			input.TCP.ClientIdleTimeout = scenarioRuntimeValue(time.Duration(0))
			input.Resources.LoadBalancer.HalfOpenIdleTimeout = scenarioRuntimeValue(time.Millisecond)
			input.Resources.LoadBalancer.EstablishedIdleTimeout = &establishedIdle
			input.Workload.Defaults = scenario.StreamConfig{Bytes: 1, PacketGap: 2 * time.Minute}
			world := New(normalizedRuntimeScenario(t, input))
			t.Cleanup(world.Stop)

			// Handshake finishes quickly; no data or ACK traffic refreshes its idle clock.
			runIngressUntil(t, world, 5*time.Second-time.Nanosecond)
			beforeCleanup := onlyRTOClient(t, world)
			if !beforeCleanup.Active || !beforeCleanup.State.Transfer.HandshakeCompleted {
				t.Fatalf("established connection closed before its cleanup tick: %+v", beforeCleanup)
			}
			if len(readWorld(world).Routes) != 1 {
				t.Fatal("the established route expired before its cleanup tick")
			}

			if establishedIdle == 0 {
				runIngressUntil(t, world, input.Execution.Duration)
				if observed := onlyRTOClient(t, world); !observed.Active {
					t.Fatalf("disabled established idle timer still closed the client: %+v", observed)
				}
				if len(readWorld(world).Routes) != 1 {
					t.Fatal("disabled LB cleanup still expired the established route")
				}
				return
			}

			runIngressUntil(t, world, 5*time.Second)
			observed := onlyRTOClient(t, world)
			transfer := observed.State.Transfer
			if !observed.Active || transfer.FailureReason != "" ||
				transfer.Elapsed != 5*time.Second {
				t.Fatalf("LB cleanup changed the client: %+v", observed)
			}
			if len(readWorld(world).Routes) != 0 || world.lb.GetServers()[0].ActiveConnections != 1 {
				t.Fatal("LB cleanup did not expire only its own route")
			}
		})
	}
}

// A 1-byte transfer crosses real modeled links and supplies positive SYN/data RTTs.
// Fixed mode must preserve the scenario's 375 ms timeout rather than the 1 s default.
func TestWorldRTOMeasuresPhysicalTopologyAndUsesFixedConfiguration(t *testing.T) {
	for _, mode := range []model.RTOMode{model.RTOAdaptive, model.RTOFixed} {
		t.Run(string(mode), func(t *testing.T) {
			input := scenarioRuntimeDefinition()
			input.Execution.Duration = time.Second
			input.Resources.Backends = input.Resources.Backends[:1]
			input.Network.Defaults.PacketTravelTime = 10 * time.Millisecond
			input.Workload.Defaults.Bytes = 1

			wantTimeout := 200 * time.Millisecond
			if mode == model.RTOFixed {
				wantTimeout = 375 * time.Millisecond
				input.TCP.RTO = scenario.RTOConfig{
					Mode:               scenarioRuntimeValue(string(mode)),
					ExponentialBackoff: scenarioRuntimeValue(false),
					InitialTimeout:     &wantTimeout,
					MinimumTimeout:     scenarioRuntimeValue(125 * time.Millisecond),
					MaximumTimeout:     scenarioRuntimeValue(2 * time.Second),
				}
			}
			world := New(normalizedRuntimeScenario(t, input))
			t.Cleanup(world.Stop)
			runIngressUntil(t, world, input.Execution.Duration)

			observed := onlyRTOClient(t, world)
			state := observed.State.RTO
			if !observed.Completed || observed.State.Transfer.Status != model.TransferSucceeded {
				t.Fatalf("clean physical transfer did not complete: %+v", observed)
			}
			if state.Samples != 2 || state.LatestRTT <= 0 || state.SmoothedRTT <= 0 {
				t.Fatalf("physical link delays were not reflected in RTT samples: %+v", state)
			}
			if state.BaseRTO != wantTimeout || state.CurrentRTO != wantTimeout {
				t.Fatalf("runtime RTO = %+v, want timeout %s", state, wantTimeout)
			}
		})
	}
}

// Inspect the public observation instead of reaching into the runtime client's timers.
func onlyRTOClient(t *testing.T, world *World) ClientObservation {
	t.Helper()
	var result ClientObservation
	count := 0
	world.VisitClients(func(observed ClientObservation) {
		result = observed
		count++
	})
	if count != 1 {
		t.Fatalf("observed %d clients, want 1", count)
	}
	return result
}
