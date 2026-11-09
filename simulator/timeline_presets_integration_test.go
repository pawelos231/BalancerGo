package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// Keep the catalog's links, hosts, TCP and budgets, but move 30/90 s to
// 100/300 ms, use 8 MiB streams and a 600 ms horizon. The trickle uses an
// exact 10 ms gap: twenty births precede removal at 300 ms, the next is canceled.
func shortTimelinePreset(t *testing.T, id string) scenario.ScenarioV2 {
	t.Helper()
	document, err := os.ReadFile(filepath.Join("..", "examples", "scenarios", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := scenario.DecodeDocumentV2(document)
	if err != nil {
		t.Fatal(err)
	}
	input.Fixed.Execution.Duration = 600 * time.Millisecond
	input.Fixed.Execution.SnapshotInterval = 10 * time.Millisecond
	input.Fixed.Execution.Speed = 0
	input.Fixed.Execution.MaxEvents = 5_000_000
	input.Fixed.Workload.Defaults.Bytes = 8 << 20
	states := make(map[string]scenario.StateConfig, 3)
	for _, phase := range []struct{ source, target string }{
		{"0", "0"}, {"30000000000", "100000000"}, {"90000000000", "300000000"},
	} {
		state, ok := input.States[phase.source]
		if !ok {
			t.Fatalf("preset %s is missing its %s phase", id, phase.source)
		}
		for index := range state.Workload.ClientGroups {
			group := &state.Workload.ClientGroups[index]
			group.StartAt /= 300
			if group.ClientGap > 0 {
				group.ClientGap = 10 * time.Millisecond
			}
		}
		states[phase.target] = state
	}
	input.States = states
	return input
}

func assertTimelinePresetPhase(t *testing.T, id string, document contract.TopologyDocument, recovery bool) {
	t.Helper()
	switch id {
	case "timeline-lb-capacity":
		want := int64(100_000_000)
		if recovery {
			want = 1_000_000_000
		}
		if document.Ingress == nil || document.Ingress.Config.BandwidthBitsPerSecond != want {
			t.Fatalf("shared ingress did not reach its later phase: %+v", document.Ingress)
		}
		assertTimelinePresetMetric(t, document, "forwardingBitsPerSecond", float64(want))

	case "timeline-cpu-scaling":
		cores := float64(1)
		if recovery {
			cores = 4
		}
		assertTimelinePresetMetric(t, document, "hostCPUEnabled", 1)
		assertTimelinePresetMetric(t, document, "hostCores", cores)
		assertTimelinePresetMetric(t, document, "hostPacketProcessingTimeUs", 10)
	}
}

func assertTimelinePresetMetric(t *testing.T, document contract.TopologyDocument, key string, want float64) {
	t.Helper()
	for _, node := range document.Nodes {
		if node.ID != simulation.LoadBalancerNodeID {
			continue
		}
		for _, metric := range node.Metrics {
			if metric.Key == key && metric.Value != nil && *metric.Value == want {
				return
			}
		}
	}
	t.Fatalf("later LB definition is missing %s=%v", key, want)
}

func TestTimelinePresetsExecuteEveryPhaseThroughHTTPAndCLI(t *testing.T) {
	executable := buildScenarioCLI(t)
	for _, id := range []string{"timeline-arrival-burst", "timeline-lb-capacity", "timeline-cpu-scaling"} {
		t.Run(id, func(t *testing.T) {
			h := newScenarioHarness(t)
			// Freeze a separate run inside the bottleneck. Its terminal envelope
			// binds the middle-phase definitions and counters without racing a
			// later recovery or depending on delivery of replaceable telemetry.
			prefix := shortTimelinePreset(t, id)
			prefix.Fixed.Execution.Duration = 250 * time.Millisecond
			delete(prefix.States, "300000000")
			prefixReceipt := startTelemetryScenario(t, h, prefix)
			middleEnd := readLifecycleEnd(t, h.socket, prefixReceipt.RunID)
			assertLifecycleMeasurement(t, middleEnd, contract.SimulationFinished)
			middle := middleEnd.FinalSnapshot
			var middleDefinition contract.TopologyDocument
			decodeLifecycleMessage(t, middleEnd.TopologyDefinition, &middleDefinition)
			assertTimelinePresetPhase(t, id, middleDefinition, false)
			if middle.Simulation.Payload == nil || middle.Simulation.Payload.AcknowledgedBytes == 0 ||
				middle.Simulation.Transfers == nil || middle.Simulation.Transfers.InProgress == 0 {
				t.Fatal("the bottleneck phase had no live payload")
			}

			input := shortTimelinePreset(t, id)
			document, err := scenario.EncodeDocumentV2(input)
			if err != nil {
				t.Fatal(err)
			}
			scenarioRequest(t, h.server, http.MethodPost, "/api/scenarios/validate", document, http.StatusOK)
			receipt := startTelemetryScenario(t, h, input)
			ended := readLifecycleEnd(t, h.socket, receipt.RunID)
			assertLifecycleMeasurement(t, ended, contract.SimulationFinished)
			final := ended.FinalSnapshot
			if final.Simulation.Status != "finished" || final.Simulation.VirtualTimeSeconds != 0.6 ||
				final.Simulation.Payload == nil || final.Simulation.Transfers == nil ||
				final.Simulation.Payload.AcknowledgedBytes <= middle.Simulation.Payload.AcknowledgedBytes ||
				final.Simulation.Transfers.InProgress == 0 || final.Simulation.Transfers.Failed != 0 {
				t.Fatalf("payload did not survive through recovery: %+v", final.Simulation)
			}
			var definition contract.TopologyDocument
			decodeLifecycleMessage(t, ended.TopologyDefinition, &definition)
			assertTimelinePresetPhase(t, id, definition, true)

			clients := 0
			for _, node := range definition.Nodes {
				if node.Kind == contract.TopologyNodeClient {
					clients++
				}
			}
			wantClients := 32
			if id == "timeline-arrival-burst" {
				wantClients = 52 // 8 baseline + 24 burst + 20 trickle births.
			}
			if clients != wantClients || final.Simulation.Transfers.Started != wantClients {
				t.Fatalf("later generators lost born clients or created stopped slots: nodes=%d transfers=%+v", clients, final.Simulation.Transfers)
			}

			body := scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusOK)
			var record contract.RunRecord
			decodeLifecycleMessage(t, body, &record)
			if record.RunID != receipt.RunID || record.Status != "finished" || !bytes.Equal(record.Scenario, document) {
				t.Fatal("HTTP did not preserve the admitted full timeline")
			}
			assertInteractiveExperiment(t, document, 0, record.Experiment)

			// The executable receives the same complete document and runs it unpaced.
			path := writeScenarioCLIInput(t, input)
			report := decodeScenarioCLIReport(t, executeScenarioCLI(t, executable, "-scenario", path), "finished")
			assertScenarioCLIManifest(t, report.Manifest, input, false)
			var manifest contract.ExperimentManifest
			if err := json.Unmarshal(report.Manifest, &manifest); err != nil {
				t.Fatal(err)
			}
			if final.Simulation.Experiment == nil ||
				final.Simulation.Experiment.ScenarioSHA256 != record.Experiment.ScenarioSHA256 ||
				manifest.ScenarioSHA256 != record.Experiment.ScenarioSHA256 ||
				report.Result.CreatedClients != wantClients ||
				report.Result.ReceivedPayloadBytes != final.Simulation.Payload.ReceivedBytes ||
				report.Result.AcknowledgedPayloadBytes != final.Simulation.Payload.AcknowledgedBytes {
				t.Fatal("CLI report, HTTP source and final WS measurement disagree")
			}

			if id == "timeline-cpu-scaling" {
				host := report.Result.LoadBalancerHost
				if host == nil || host.Cores != 4 || host.BusyTimeSeconds <= 0 || host.QueueDroppedPackets == 0 {
					t.Fatal("the full run missed the one-core CPU overload before scaling")
				}
			}
			if id == "timeline-lb-capacity" {
				control := shortTimelinePreset(t, id)
				delete(control.States, "100000000")
				delete(control.States, "300000000")
				controlPath := writeScenarioCLIInput(t, control)
				static := decodeScenarioCLIReport(t, executeScenarioCLI(t, executable, "-scenario", controlPath), "finished")
				if report.Result.AcknowledgedPayloadBytes >= static.Result.AcknowledgedPayloadBytes {
					t.Fatal("the full run missed the temporary shared-capacity decrease")
				}
			}
		})
	}
}

func TestTimelinePresetInvalidRecoveryIsRejectedBeforeAdmission(t *testing.T) {
	h := newScenarioHarness(t)
	input := shortTimelinePreset(t, "timeline-lb-capacity")
	recovery := input.States["300000000"]
	recovery.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = -1
	input.States["300000000"] = recovery
	document, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/scenarios/validate", "/api/simulations"} {
		scenarioRequest(t, h.server, http.MethodPost, path, document, http.StatusUnprocessableEntity)
	}
	scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
	if h.control.currentRun() != nil || h.control.contextForRun() != nil {
		t.Fatal("invalid recovery reserved a run")
	}
}
