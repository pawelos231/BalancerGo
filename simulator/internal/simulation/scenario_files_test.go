package simulation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"load-balancer/scenario"
)

// TestScenarioFilesRoundTrip checks the shipped files without running their full workloads.
func TestScenarioFilesRoundTrip(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "examples", "scenarios", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no scenario example files found")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			document, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			input, err := scenario.DecodeDocument(document)
			if err != nil {
				t.Fatalf("strictly decode example: %v", err)
			}
			original, err := scenario.Normalize(input)
			if err != nil {
				t.Fatalf("normalize example: %v", err)
			}

			// Canonical transport must preserve every effective setting of the full file.
			restored := scenarioFileRoundTrip(t, input)
			resolved, err := scenario.Normalize(restored)
			if err != nil {
				t.Fatalf("normalize restored example: %v", err)
			}
			if !reflect.DeepEqual(original, resolved) {
				t.Fatal("file round-trip changed the complete normalized configuration")
			}

			// Change only detached copies; retain the example's links, groups and seed.
			if !limitScenarioSmokeClients(t, &restored) {
				return
			}
			restored.Execution.Duration = 50 * time.Millisecond
			restored.Execution.SnapshotInterval = time.Millisecond
			restored.Execution.Speed = 0
			restored.Execution.MaxEvents = 1_000_000
			restored.Workload.Defaults.Bytes = 2050
			for index := range restored.Workload.ClientGroups {
				group := &restored.Workload.ClientGroups[index]
				if group.Stream.Bytes != nil {
					*group.Stream.Bytes = 2050
				}
				for position := range group.Overrides {
					if group.Overrides[position].Stream.Bytes != nil {
						*group.Overrides[position].Stream.Bytes = 2050
					}
				}
			}

			first := runScenarioFile(t, restored)
			second := runScenarioFile(t, scenarioFileRoundTrip(t, restored))
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("file round-trip changed virtual results:\nfirst: %+v\nsecond: %+v", first, second)
			}
		})
	}
}

// limitScenarioSmokeClients bounds runtime allocation without discarding explicit overrides.
func limitScenarioSmokeClients(t *testing.T, input *scenario.Scenario) bool {
	t.Helper()
	const maxClients = 128
	groups := input.Workload.ClientGroups
	total, required := 0, 0
	minimum := make([]int, len(groups))
	for index, group := range groups {
		total += group.Count
		for _, override := range group.Overrides {
			minimum[index] = max(minimum[index], override.Index+1)
		}
		required += minimum[index]
	}
	if total <= maxClients {
		return true
	}
	if required > maxClients {
		t.Logf("runtime smoke omitted: explicit override indices require %d clients, above the %d-client limit; full document round-trip passed", required, maxClients)
		return false
	}

	// Reserve override prefixes first, then fill remaining slots in document order.
	remaining := maxClients - required
	for index := range groups {
		extra := min(groups[index].Count-minimum[index], remaining)
		groups[index].Count = minimum[index] + extra
		remaining -= extra
	}
	return true
}

// TestScenarioSmokeSampleKeepsOverrides checks the global cap across multiple client groups.
func TestScenarioSmokeSampleKeepsOverrides(t *testing.T) {
	input := scenarioRuntimeDefinition()
	input.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "first", Count: 100, Overrides: []scenario.ClientOverride{{Index: 75}}},
		{ID: "second", Count: 100, Overrides: []scenario.ClientOverride{{Index: 40}}},
	}
	if !limitScenarioSmokeClients(t, &input) {
		t.Fatal("a sample that can retain both override prefixes was rejected")
	}
	resolved := normalizedRuntimeScenario(t, input)
	if resolved.TotalClients != 128 || resolved.ClientGroups[0].Count != 87 || resolved.ClientGroups[1].Count != 41 {
		t.Fatal("the sample did not reserve override prefixes before filling its global budget")
	}
	if resolved.ClientGroups[0].Overrides[0].Index != 75 || resolved.ClientGroups[1].Overrides[0].Index != 40 {
		t.Fatal("the bounded sample changed explicit client identities")
	}
}

// TestScenarioLimitsDocument checks the complete stress document without constructing its endpoints.
func TestScenarioLimitsDocument(t *testing.T) {
	path := filepath.Join("..", "..", "..", "examples", "scenarios", "simulator-limits.json")
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Resources struct {
			LoadBalancer struct {
				Forwarding json.RawMessage `json:"forwardingBitsPerSecond"`
				Ingress    struct {
					Bandwidth json.RawMessage `json:"bandwidthBitsPerSecond"`
				} `json:"ingress"`
			} `json:"loadBalancer"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(document, &wire); err != nil {
		t.Fatal(err)
	}
	exactRate := []byte(`"80000000000000000"`)
	if !bytes.Equal(wire.Resources.LoadBalancer.Forwarding, exactRate) || !bytes.Equal(wire.Resources.LoadBalancer.Ingress.Bandwidth, exactRate) {
		t.Fatal("both LB rates must use exact decimal strings outside the JavaScript safe-integer range")
	}
	input, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	resolved := normalizedRuntimeScenario(t, input)
	if resolved.TotalClients != 50_000 || len(resolved.Backends) != 100 || len(resolved.ClientGroups) != 1 {
		t.Fatal("the stress document must retain all 50,000 clients in one compact group and 100 backends")
	}
	if resolved.LoadBalancer.ForwardingBitsPerSecond != 80_000_000_000_000_000 || resolved.LoadBalancer.Ingress.BandwidthBitsPerSecond != 80_000_000_000_000_000 {
		t.Fatal("normalization rounded the LB stress rates")
	}
	if resolved.LoadBalancer.Ingress.QueueCapacity != 131_072 || resolved.ClientGroups[0].Count != 50_000 {
		t.Fatal("the full stress ingress capacity or compact client count changed")
	}
	for _, backend := range resolved.Backends {
		if backend.Network.ToLoadBalancer.BandwidthBitsPerSecond != 800_000_000_000_000 || backend.Network.FromLoadBalancer.BandwidthBitsPerSecond != 800_000_000_000_000 {
			t.Fatal("a backend did not receive its independent directional stress profile")
		}
	}
}

// scenarioFileRoundTrip produces independent input data through the public document codec.
func scenarioFileRoundTrip(t *testing.T, input scenario.Scenario) scenario.Scenario {
	t.Helper()
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	return restored
}

// runScenarioFile excludes wall time while retaining all virtual counters and endpoint state.
func runScenarioFile(t *testing.T, input scenario.Scenario) worldReadings {
	t.Helper()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	s := New(resolved)
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	result := readWorld(s)
	if s.engine.Now() != input.Execution.Duration {
		t.Fatal("the bounded run did not reach its virtual horizon")
	}

	// Arrival chains may start after this shortened horizon, including every group.
	expectedCreated := 0
	for _, group := range resolved.ClientGroups {
		if group.Count == 0 || group.StartAt > input.Execution.Duration {
			continue
		}
		count := group.Count
		if group.ClientGap > 0 {
			lastIndex := int64((input.Execution.Duration - group.StartAt) / group.ClientGap)
			if lastIndex < int64(count-1) {
				count = int(lastIndex) + 1
			}
		}
		expectedCreated += count
	}
	if result.CreatedClients != expectedCreated {
		t.Fatalf("created clients = %d, want %d within the bounded horizon", result.CreatedClients, expectedCreated)
	}
	if expectedCreated == 0 {
		if result.Forwarding.ForwardedPackets != 0 || result.Ingress.OfferedPackets != 0 || result.CompletedStreams != 0 {
			t.Fatal("an empty workload produced traffic or completed streams")
		}
		return result
	}
	if result.EventsExecuted == 0 {
		t.Fatal("created clients did not execute arrival or TCP events")
	}
	if len(resolved.Backends) == 0 {
		if result.Forwarding.ForwardedPackets != 0 || result.CompletedStreams != 0 {
			t.Fatal("clients without backends forwarded packets or completed streams")
		}
	} else if result.Ingress.DeliveredPackets > 0 && result.Forwarding.ForwardedPackets == 0 {
		t.Fatal("delivered client traffic did not exercise LB forwarding")
	}
	return result
}
