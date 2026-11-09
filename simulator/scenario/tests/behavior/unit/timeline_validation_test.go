package scenario_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

// Direct Go callers must pass the same timestamp rules as JSON documents.
func TestNormalizeChecksTimelineBounds(t *testing.T) {
	tests := []struct {
		name string
		edit func(*scenario.Scenario)
	}{
		{"wrong version", func(input *scenario.Scenario) { input.ScenarioVersion = 1 }},
		{"missing zero", func(input *scenario.Scenario) { delete(input.States, "0") }},
		{"nil states", func(input *scenario.Scenario) { input.States = nil }},
		{"zero duration", func(input *scenario.Scenario) { input.Fixed.Execution.Duration = 0 }},
		{"negative duration", func(input *scenario.Scenario) { input.Fixed.Execution.Duration = -1 }},
		{"at duration", func(input *scenario.Scenario) {
			input.Fixed.Execution.Duration = 2
			input.States["2"] = exampleTimeline().States["0"]
		}},
		{"after duration", func(input *scenario.Scenario) {
			input.Fixed.Execution.Duration = 2
			input.States["3"] = exampleTimeline().States["0"]
		}},
	}

	for _, key := range []string{"", "-1", "+1", "01", "1.0", "1e2", "9223372036854775808"} {
		tests = append(tests, struct {
			name string
			edit func(*scenario.Scenario)
		}{
			name: "invalid timestamp " + key,
			edit: func(input *scenario.Scenario) {
				input.States[key] = exampleTimeline().States["0"]
			},
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleTimeline()
			test.edit(&input)
			if _, err := scenario.Normalize(input); err == nil {
				t.Fatal("invalid timeline was accepted")
			}
		})
	}
}

// Nanosecond keys above JavaScript's safe integer range must not round to a neighbor.
func TestNormalizePreservesExactLargeTimestamps(t *testing.T) {
	input := exampleTimeline()
	input.Fixed.Execution.Duration = 9_007_199_254_740_994
	input.States["9007199254740993"] = exampleTimeline().States["0"]

	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.States[1].At != 9_007_199_254_740_993 {
		t.Fatalf("timestamp lost precision: %d", resolved.States[1].At)
	}
}

// Keeping an ID means keeping its endpoint or generator identity for the whole run.
func TestNormalizeRejectsIdentityChangesAndReuse(t *testing.T) {
	tests := []struct {
		name        string
		edit        func(*scenario.Scenario)
		returnAtTen bool
		errorText   string
	}{
		{
			name:      "backend address changed",
			edit:      func(input *scenario.Scenario) { input.States["2"].Resources.Backends[0].Address = "10.0.0.2" },
			errorText: "retained backend address and port must not change",
		},
		{
			name:      "backend port changed",
			edit:      func(input *scenario.Scenario) { input.States["2"].Resources.Backends[0].Port = 81 },
			errorText: "retained backend address and port must not change",
		},
		{
			name:      "group start changed",
			edit:      func(input *scenario.Scenario) { input.States["2"].Workload.ClientGroups[0].StartAt = 1 },
			errorText: "retained group start time must not change",
		},
		{
			name:      "new group starts in the past",
			edit:      func(input *scenario.Scenario) { input.States["2"].Workload.ClientGroups[0].ID = "new-clients" },
			errorText: "new group must start at or after its insertion time",
		},
		{
			name: "zero-budget new group starts in the past",
			edit: func(input *scenario.Scenario) {
				group := &input.States["2"].Workload.ClientGroups[0]
				group.ID, group.Count = "new-clients", 0
			},
			errorText: "new group must start at or after its insertion time",
		},
		{
			name: "backend disappears then returns",
			edit: func(input *scenario.Scenario) {
				state := input.States["2"]
				state.Resources.Backends = nil
				input.States["2"] = state
			},
			returnAtTen: true,
			errorText:   "cannot be reused after removal",
		},
		{
			name: "group disappears then returns",
			edit: func(input *scenario.Scenario) {
				state := input.States["2"]
				state.Workload.ClientGroups = nil
				input.States["2"] = state
			},
			returnAtTen: true,
			errorText:   "cannot be reused after removal",
		},
		{
			name: "removed backend ID becomes a group",
			edit: func(input *scenario.Scenario) {
				state := input.States["2"]
				state.Resources.Backends = nil
				state.Workload.ClientGroups[0].ID = "backend"
				state.Workload.ClientGroups[0].StartAt = 2
				input.States["2"] = state
			},
			errorText: "already belongs to",
		},
		{
			name: "removed group ID becomes a backend",
			edit: func(input *scenario.Scenario) {
				state := input.States["2"]
				state.Workload.ClientGroups = nil
				state.Resources.Backends[0].ID = "clients"
				input.States["2"] = state
			},
			errorText: "already belongs to",
		},
		{
			name:      "backend takes the LB ID",
			edit:      func(input *scenario.Scenario) { input.States["2"].Resources.Backends[0].ID = "lb" },
			errorText: "already used by fixed.loadBalancer.id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleTimeline()
			input.States["2"] = exampleTimeline().States["0"]

			// Only removal/reappearance cases need a third state restoring the old ID.
			errorState := `states["2"]`
			if test.returnAtTen {
				input.States["10"] = exampleTimeline().States["0"]
				errorState = `states["10"]`
			}
			test.edit(&input)

			_, err := scenario.Normalize(input)
			if err == nil || !strings.Contains(err.Error(), errorState) || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("want %q at %s, got %v", test.errorText, errorState, err)
			}
		})
	}
}

// Names, limits and policies may change while IDs and endpoints remain stable.
func TestNormalizeAllowsMutableSettingsAndEmptyResources(t *testing.T) {
	input := exampleTimeline()
	state := exampleTimeline().States["0"]
	state.Resources.LoadBalancer.Algorithm = "random"
	state.Resources.LoadBalancer.ForwardingBitsPerSecond = 1
	state.Resources.LoadBalancer.Host.Cores = pointer(2)
	state.Resources.Backends[0].Name = "new label"
	state.Resources.Backends[0].Network.ToLoadBalancer.BandwidthBitsPerSecond = pointer[int64](1)
	state.Workload.ClientGroups[0].Count = 0
	state.Workload.ClientGroups[0].ClientGap = time.Second
	input.States["2"] = state

	// No available backend and no future arrivals are legal experimental conditions.
	empty := exampleTimeline().States["0"]
	empty.Resources.Backends = nil
	empty.Workload.ClientGroups = nil
	input.States["10"] = empty

	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.States[1].LoadBalancer.Algorithm != "random" || resolved.States[1].Backends[0].Name != "new label" {
		t.Fatal("mutable settings were lost")
	}
	if len(resolved.States[2].Backends) != 0 || resolved.States[2].TotalClients != 0 || resolved.TotalClients != 10 {
		t.Fatal("empty state must retain the run's historical client reservation")
	}
}

// Equivalent IPv6 spelling and host casing are not endpoint identity changes.
func TestNormalizeComparesCanonicalBackendHosts(t *testing.T) {
	for _, addresses := range [][2]string{
		{"2001:db8::1", "2001:0db8:0:0:0:0:0:1"},
		{"Backend.Example", "backend.example"},
	} {
		t.Run(addresses[0], func(t *testing.T) {
			input := exampleTimeline()
			input.States["0"].Resources.Backends[0].Address = addresses[0]
			input.States["2"] = exampleTimeline().States["0"]
			input.States["2"].Resources.Backends[0].Address = addresses[1]

			resolved, err := scenario.Normalize(input)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.States[1].Backends[0].Address != addresses[1] {
				t.Fatal("canonical comparison changed the supplied address spelling")
			}
		})
	}
}

// A lower birth budget can still configure reserved client 9 from the earlier budget of 10.
func TestNormalizeRetainsHistoricalClientSlots(t *testing.T) {
	input := exampleTimeline()
	state := exampleTimeline().States["0"]
	group := &state.Workload.ClientGroups[0]
	group.Count = 1
	group.Overrides = []scenario.ClientOverride{{Index: 9}}
	input.States["2"] = state

	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	finalState := resolved.States[1]
	if resolved.TotalClients != 10 || finalState.TotalClients != 1 || len(finalState.ClientGroups[0].Overrides) != 1 {
		t.Fatal("count reduction discarded historical reservations or overrides")
	}

	// A future increase cannot make an invalid earlier index valid retroactively.
	input.States["0"].Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{{Index: 10}}
	state.Workload.ClientGroups[0].Count = 11
	input.States["2"] = state
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("an override used a client slot reserved only by a later state")
	}
}

// Removed groups do not free client addresses; limits apply across the entire run.
func TestNormalizeChecksRunWideClientReservations(t *testing.T) {
	input := exampleTimeline()
	input.States["0"].Workload.ClientGroups[0].Count = scenario.MaxClients

	// A maximal compact group must not expand into millions of normalized entries.
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TotalClients != scenario.MaxClients || len(resolved.States[0].ClientGroups) != 1 {
		t.Fatal("maximal group did not retain its compact representation")
	}

	state := exampleTimeline().States["0"]
	group := &state.Workload.ClientGroups[0]
	group.ID, group.StartAt, group.Count = "replacement", 2, 1
	input.States["2"] = state
	if _, err := scenario.Normalize(input); err == nil {
		t.Fatal("a removed group's reserved addresses were incorrectly released")
	}

	// Growing an existing group consumes only the increase, not its entire count again.
	input = exampleTimeline()
	state = exampleTimeline().States["0"]
	state.Workload.ClientGroups[0].Count = 11
	input.States["2"] = state
	resolved, err = scenario.Normalize(input)
	if err != nil || resolved.TotalClients != 11 {
		t.Fatalf("reservation after growth = %d, want 11; error: %v", resolved.TotalClients, err)
	}
}

// Far-future arrivals and a huge gap need no overflowing start+count*gap calculation.
func TestNormalizeAllowsArrivalsBeyondRunEnd(t *testing.T) {
	input := exampleTimeline()
	group := &input.States["0"].Workload.ClientGroups[0]
	group.StartAt = time.Duration(math.MaxInt64)
	group.ClientGap = time.Duration(math.MaxInt64)
	if _, err := scenario.Normalize(input); err != nil {
		t.Fatal(err)
	}
}
