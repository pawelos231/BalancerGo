package main

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// TestTransferResultsPartitionAttempts counts failures separately from active and future work.
func TestTransferResultsPartitionAttempts(t *testing.T) {
	input := payloadScenario()
	input.Execution.Speed = 0
	input.Execution.Duration = 60 * time.Millisecond
	input.Workload.ClientGroups = []scenario.ClientGroup{
		{
			ID: "completed", Count: 1,
			Stream: scenario.StreamPatch{
				Bytes: scenarioRuntimeValue(17), PacketGap: scenarioRuntimeValue(time.Duration(0)),
			},
		},
		{ID: "interrupted", Count: 1},
		{ID: "active", Count: 1, Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(1 << 20)}},
		{ID: "future", Count: 2, StartAt: time.Second},
	}
	s := simulation.New(normalizedRuntimeScenario(t, input))
	before := presentation.TransferResults(s)
	if before.Started != 0 || before.NotStarted != 5 || before.Handshake.Samples != 0 {
		t.Fatalf("before arrivals = %+v", before)
	}
	s.ScheduleAt(19*time.Millisecond, func() {
		s.VisitClients(func(runtime simulation.ClientObservation) {
			if runtime.GroupID == "interrupted" {
				if err := s.StopClient(runtime.ID); err != nil {
					t.Fatal(err)
				}
			}
		})
	})
	if err := runWorld(context.Background(), s, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	// One short transfer succeeds, one is interrupted, and the long transfer reaches the horizon.
	result := presentation.BuildResult(s, 0)
	summary := result.Transfers
	if summary.Started != 3 || summary.Succeeded != 1 || summary.Failed != 1 ||
		summary.InProgress != 1 || summary.NotStarted != 2 {
		t.Fatalf("transfer partition = %+v", summary)
	}
	if summary.Succeeded+summary.Failed+summary.InProgress != summary.Started ||
		summary.Started+summary.NotStarted != s.Info().ScheduledClients {
		t.Fatal("attempt counts do not balance")
	}
	if summary.Failures[contract.TransferInterrupted] != 1 || len(summary.Failures) != 1 {
		t.Fatalf("failure reasons = %v", summary.Failures)
	}
	if result.IncompleteStreams != 2 {
		t.Fatal("legacy incomplete count must still include both failed and active streams")
	}
	if summary.Handshake.Samples != 3 || summary.Payload.Samples != 1 || summary.Completion.Samples != 1 {
		t.Fatal("duration samples included unfinished payloads or omitted a completed handshake")
	}
	if summary.Completion.MeanSeconds <= summary.Payload.MeanSeconds ||
		summary.Handshake.MinSeconds <= 0 || summary.Handshake.MaxSeconds < summary.Handshake.MeanSeconds {
		t.Fatalf("phase durations are inconsistent: %+v", summary)
	}

	// Reading summaries is pure; later cleanup retains earlier, detached results.
	executed := s.EventsExecuted()
	if !reflect.DeepEqual(summary, presentation.TransferResults(s)) || s.EventsExecuted() != executed {
		t.Fatal("reading transfer results advanced or changed the model")
	}
	telemetry := payloadTelemetry(t, presentation.NewProjector("mixed-transfers"), s)
	if !reflect.DeepEqual(telemetry.Transfers, summary) ||
		telemetry.Topology.Nodes[0].Metrics["failedTransfers"] != 1 ||
		telemetry.Topology.Nodes[0].Metrics["inProgressTransfers"] != 1 {
		t.Fatal("live transfer results differ from headless results")
	}
	s.ClearConnections()
	after := presentation.TransferResults(s)
	if after.Failed != 2 || after.InProgress != 0 || after.Succeeded != 1 || summary.Failed != 1 ||
		summary.Failures[contract.TransferInterrupted] != 1 {
		t.Fatal("cleanup discarded terminal outcomes or mutated an earlier result")
	}
}

// TestTransferResultsRetainUnroutedFailures includes clients absent from the LB's routing table.
func TestTransferResultsRetainUnroutedFailures(t *testing.T) {
	input := payloadScenario()
	input.Resources.Backends = nil
	// Isolate the client's 20 s read timeout from LB half-open cleanup.
	input.Resources.LoadBalancer.HalfOpenIdleTimeout = scenarioRuntimeValue(time.Duration(0))
	input.Execution.Duration = 21 * time.Second
	input.Execution.Speed = 0
	s := runTransferWorld(t, input)
	summary := presentation.TransferResults(s)
	if summary.Started != 1 || summary.Failed != 1 || summary.InProgress != 0 || summary.Succeeded != 0 {
		t.Fatalf("unrouted attempts = %+v", summary)
	}
	if summary.Failures[contract.TransferReadTimeout] != 1 || summary.SynRetransmissions == 0 ||
		summary.DataRetransmissions != 0 || summary.Handshake.Samples != 0 || summary.Completion.Samples != 0 {
		t.Fatalf("unrouted timeout and retry accounting = %+v", summary)
	}
	routes := 0
	s.VisitRoutes(func(simulation.RouteObservation) { routes++ })
	if payloadClients(s)[0].Active || routes != 0 {
		t.Fatal("the failed client did not leave the live routing table")
	}
}

// TestTransferCompletionAtHorizon keeps an exact-boundary success and a known zero payload duration.
func TestTransferCompletionAtHorizon(t *testing.T) {
	for _, end := range []time.Duration{5 * time.Nanosecond, 6 * time.Nanosecond} {
		t.Run(end.String(), func(t *testing.T) {
			input := scenarioRuntimeDefinition()
			input.Resources.Backends = input.Resources.Backends[:1]
			input.Network.Defaults.BandwidthBitsPerSecond = math.MaxInt64
			input.Resources.LoadBalancer.Ingress = input.Network.Defaults
			input.Execution.Duration = end
			input.Execution.SnapshotInterval = time.Nanosecond
			input.Execution.Speed = 0
			s := runTransferWorld(t, input)
			summary := presentation.TransferResults(s)
			if end == 5*time.Nanosecond {
				if summary.Succeeded != 0 || summary.InProgress != 1 || summary.Payload.Samples != 0 {
					t.Fatalf("before final handshake arrival = %+v", summary)
				}
				return
			}
			if summary.Succeeded != 1 || summary.Failed != 0 || summary.InProgress != 0 ||
				summary.Payload.Samples != 1 || summary.Payload.MeanSeconds != 0 ||
				summary.Completion.Samples != 1 || summary.Completion.MeanSeconds != end.Seconds() {
				t.Fatalf("completion at the inclusive horizon = %+v", summary)
			}
			if !payloadClients(s)[0].Active {
				t.Fatal("fixture should succeed before the FIN exchange can finish")
			}
		})
	}
}

// TestTransferBackendRemovalRecordsExistingInterruption annotates removal without waiting for a new timer.
func TestTransferBackendRemovalRecordsExistingInterruption(t *testing.T) {
	s := simulation.New(normalizedRuntimeScenario(t, payloadScenario()))
	runIngressUntil(t, s, 17*time.Millisecond)
	var backendID string
	s.VisitBackends(func(backend simulation.BackendObservation) { backendID = backend.State.ID })
	if err := s.RemoveBackend(backendID); err != nil {
		t.Fatal(err)
	}
	summary := presentation.TransferResults(s)
	if summary.Failed != 1 || summary.Failures[contract.TransferBackendRemoved] != 1 ||
		summary.Handshake.Samples != 1 || summary.Payload.Samples != 0 {
		t.Fatalf("removed backend's transfer = %+v", summary)
	}
	assertPayloadProgress(t, s, 1024, 0)
}

// TestTransferSnapshotLifecycle checks success, endpoint failure, future arrivals and reset over WS.
func TestTransferSnapshotLifecycle(t *testing.T) {
	h := newScenarioHarness(t)
	idle := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.Status == "idle"
	})
	if idle.Simulation.Transfers != nil {
		t.Fatal("idle snapshot retained transfer results")
	}

	input := payloadScenario()
	input.Execution.Speed = 0
	receipt := startTelemetryScenario(t, h, input)
	finished := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	first := finished.Simulation.Transfers
	if first == nil || first.Succeeded != 1 || first.Started != 1 || first.Completion.Samples != 1 {
		t.Fatalf("successful WS result = %+v", first)
	}
	stopTelemetryScenario(t, h)

	// A normally finished run can contain a failed transfer and a future arrival.
	input.Resources.Backends = nil
	input.Resources.LoadBalancer.HalfOpenIdleTimeout = scenarioRuntimeValue(time.Duration(0))
	input.Execution.Duration = 21 * time.Second
	input.Workload.ClientGroups = append(input.Workload.ClientGroups, scenario.ClientGroup{
		ID: "future", Count: 1, StartAt: 22 * time.Second,
	})
	receipt = startTelemetryScenario(t, h, input)
	failed := readScenarioSnapshot(t, h.socket, func(snapshot contract.SimulationSnapshot) bool {
		return snapshot.Simulation.RunID == receipt.RunID && snapshot.Simulation.Status == "finished"
	})
	second := failed.Simulation.Transfers
	if second == nil || second.Failed != 1 || second.Succeeded != 0 || second.NotStarted != 1 ||
		second.Failures[contract.TransferReadTimeout] != 1 || second.SynRetransmissions == 0 {
		t.Fatalf("failed transfer WS result = %+v", second)
	}
	if first.Succeeded != 1 || first.Failed != 0 || len(first.Failures) != 0 {
		t.Fatal("the next run changed an earlier snapshot")
	}
	stopTelemetryScenario(t, h)
}

func runTransferWorld(t *testing.T, input scenario.Scenario) *simulation.World {
	t.Helper()
	world := simulation.New(normalizedRuntimeScenario(t, input))
	if err := runWorld(context.Background(), world, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if world.Now() != input.Execution.Duration {
		t.Fatal("transfer run did not reach its virtual horizon")
	}
	return world
}
