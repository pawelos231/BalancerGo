package scenario_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

// compileTimeline keeps each test focused on the changes its configuration requests.
func compileTimeline(t *testing.T, input scenario.Scenario) *scenario.Compiled {
	t.Helper()
	plan, err := scenario.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// At 10 s add a third backend and halve LB capacity; at 20 s restore capacity only.
func TestCompileBuildsTimedResourceChanges(t *testing.T) {
	input := exampleTimeline()
	initial := input.States["0"]
	initial.Resources.Backends = append(initial.Resources.Backends, scenario.BackendStateConfig{
		ID: "backend-2", Address: "10.0.0.2", Port: 80,
	})
	input.States["0"] = initial

	atTen := exampleTimeline().States["0"]
	atTen.Resources.Backends = append(atTen.Resources.Backends, initial.Resources.Backends[1])
	atTen.Resources.Backends = append(atTen.Resources.Backends, scenario.BackendStateConfig{
		ID: "backend-3", Address: "10.0.0.3", Port: 80,
	})
	atTen.Resources.LoadBalancer.ForwardingBitsPerSecond = 500_000_000
	input.States["10000000000"] = atTen

	atTwenty := atTen
	atTwenty.Resources.LoadBalancer.ForwardingBitsPerSecond = 1_000_000_000
	input.States["20000000000"] = atTwenty

	plan := compileTimeline(t, input)
	changes := plan.Transitions()
	if len(changes) != 2 || changes[0].At != 10*time.Second || changes[1].At != 20*time.Second {
		t.Fatalf("unexpected transition times: %+v", changes)
	}
	if len(changes[0].Backends.Added) != 1 || changes[0].Backends.Added[0].ID != "backend-3" {
		t.Fatal("the first transition must add only backend-3")
	}
	if len(changes[1].Backends.Added) != 0 || len(changes[1].Backends.Updated) != 0 {
		t.Fatal("restoring LB capacity must not recreate or update its backends")
	}
	for _, change := range changes {
		lb := change.LoadBalancer
		if lb == nil || !lb.ForwardingChanged || lb.IngressChanged || lb.HostChanged ||
			lb.AlgorithmChanged || lb.TimeoutsChanged {
			t.Fatalf("a capacity edit changed unrelated LB components: %+v", lb)
		}
	}

	initialState, err := plan.StateAt(0)
	if err != nil || len(initialState.Backends) != 2 {
		t.Fatalf("initial resources were not kept separately: %+v, %v", initialState, err)
	}
}

// Reordering IDs, overrides or equivalent host spellings must not restart resources.
func TestCompileIgnoresOrderingAndEquivalentEndpointSpelling(t *testing.T) {
	input := exampleTimeline()
	initial := input.States["0"]
	initial.Resources.Backends[0].Address = "Backend.Example"
	initial.Resources.Backends = append(initial.Resources.Backends, scenario.BackendStateConfig{
		ID: "other", Address: "2001:0db8:0:0:0:0:0:1", Port: 80,
	})
	initial.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{
		{Index: 2, Stream: scenario.StreamPatch{Bytes: pointer(200)}},
		{Index: 1, Stream: scenario.StreamPatch{Bytes: pointer(100)}},
	}
	initial.Workload.ClientGroups = append(initial.Workload.ClientGroups, scenario.ClientGroup{
		ID: "other-clients", Count: 1,
	})
	input.States["0"] = initial

	// Copy the collections before reordering, so the initial state remains unchanged.
	reordered := initial
	reordered.Resources.Backends = []scenario.BackendStateConfig{
		initial.Resources.Backends[1], initial.Resources.Backends[0],
	}
	reordered.Resources.Backends[0].Address = "2001:db8::1"
	reordered.Resources.Backends[1].Address = "backend.example"
	reordered.Workload.ClientGroups = []scenario.ClientGroup{
		initial.Workload.ClientGroups[1], initial.Workload.ClientGroups[0],
	}
	reordered.Workload.ClientGroups[1].Overrides = []scenario.ClientOverride{
		initial.Workload.ClientGroups[0].Overrides[1], initial.Workload.ClientGroups[0].Overrides[0],
	}
	input.States["10"] = initial
	input.States["2"] = reordered

	plan := compileTimeline(t, input)
	if len(plan.Transitions()) != 0 {
		t.Fatal("list order and equivalent endpoint spellings produced execution changes")
	}
	state, err := plan.StateAt(9)
	if err != nil || state.At != 2 || state.Backends[0].Address != "2001:db8::1" {
		t.Fatalf("a no-op declaration disappeared from the inspection view: %+v, %v", state, err)
	}
}

// Every component is marked explicitly, allowing future handlers to update just that part.
func TestCompileIdentifiesLoadBalancerAndBackendComponents(t *testing.T) {
	input := exampleTimeline()
	next := exampleTimeline().States["0"]
	lb := &next.Resources.LoadBalancer
	lb.Ingress.BandwidthBitsPerSecond = 100
	lb.ForwardingBitsPerSecond = 200
	lb.Host.Cores = pointer(2)
	lb.Algorithm = "random"
	lb.MappingTimeouts.ClosingIdleTimeout = pointer(time.Second)

	backend := &next.Resources.Backends[0]
	backend.Name = "new label"
	backend.Network.ToLoadBalancer.BandwidthBitsPerSecond = pointer[int64](300)
	backend.Host.Cores = pointer(2)
	backend.PhysicalAvailable = pointer(false)
	backend.AdministrativeState = pointer(scenario.BackendAdminDrain)
	input.States["2"] = next

	change := compileTimeline(t, input).Transitions()[0]
	if change.LoadBalancer == nil || !change.LoadBalancer.IngressChanged ||
		!change.LoadBalancer.ForwardingChanged || !change.LoadBalancer.HostChanged ||
		!change.LoadBalancer.AlgorithmChanged || !change.LoadBalancer.TimeoutsChanged {
		t.Fatalf("missing LB component changes: %+v", change.LoadBalancer)
	}
	if len(change.Backends.Updated) != 1 {
		t.Fatal("a retained backend should have one update")
	}
	update := change.Backends.Updated[0]
	if !update.LinksChanged || !update.HostChanged || !update.NameChanged ||
		!update.AvailabilityChanged || !update.AdmissionChanged {
		t.Fatalf("missing backend component changes: %+v", update)
	}

	// Changing only the label must leave the backend's CPU, links and admission alone.
	next = exampleTimeline().States["0"]
	next.Resources.Backends[0].Name = "new label"
	input.States["2"] = next
	update = compileTimeline(t, input).Transitions()[0].Backends.Updated[0]
	if !update.NameChanged || update.LinksChanged || update.HostChanged ||
		update.AvailabilityChanged || update.AdmissionChanged {
		t.Fatalf("a name edit changed unrelated backend components: %+v", update)
	}
}

// Birth timing, current links and future payloads have different lifecycle effects.
func TestCompileSeparatesClientGroupChanges(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*scenario.ClientGroup)
		replan  bool
		links   bool
		streams bool
	}{
		{
			name: "smaller budget keeps historical client overrides",
			edit: func(group *scenario.ClientGroup) {
				group.Count = 1
				group.Overrides = []scenario.ClientOverride{{Index: 9}}
			},
			replan: true,
		},
		{
			name:   "new arrival gap",
			edit:   func(group *scenario.ClientGroup) { group.ClientGap = time.Second },
			replan: true,
		},
		{
			name: "one client's link",
			edit: func(group *scenario.ClientGroup) {
				group.Overrides = []scenario.ClientOverride{{
					Index: 2,
					Network: scenario.EndpointNetworkConfig{
						FromLoadBalancer: scenario.LinkPatch{QueueCapacity: pointer(0)},
					},
				}}
			},
			links: true,
		},
		{
			name: "one future client's stream",
			edit: func(group *scenario.ClientGroup) {
				group.Overrides = []scenario.ClientOverride{{
					Index: 2, Stream: scenario.StreamPatch{Bytes: pointer(100)},
				}}
			},
			streams: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleTimeline()
			next := exampleTimeline().States["0"]
			test.edit(&next.Workload.ClientGroups[0])
			input.States["2"] = next

			changes := compileTimeline(t, input).Transitions()[0].ClientGroups
			if len(changes.Updated) != 1 || len(changes.Added) != 0 || len(changes.Stopped) != 0 {
				t.Fatalf("a retained group was recreated or stopped: %+v", changes)
			}
			update := changes.Updated[0]
			if update.ReplanArrivals != test.replan || update.LinksChanged != test.links ||
				update.StreamChanged != test.streams {
				t.Fatalf("incorrect group change flags: %+v", update)
			}
		})
	}

	// An override identical to inherited settings adds no actual work to the execution plan.
	input := exampleTimeline()
	next := exampleTimeline().States["0"]
	next.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{{Index: 2}}
	input.States["2"] = next
	if len(compileTimeline(t, input).Transitions()) != 0 {
		t.Fatal("a redundant client override produced an execution change")
	}
}

// Replacement resources are sorted by ID; a removed group stops only its generator.
func TestCompileOrdersMembershipChangesByID(t *testing.T) {
	input := exampleTimeline()
	initial := input.States["0"]
	initial.Resources.Backends = []scenario.BackendStateConfig{
		{ID: "z-old", Address: "10.0.0.1", Port: 80},
		{ID: "a-old", Address: "10.0.0.2", Port: 80},
	}
	initial.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "z-clients", Count: 1}, {ID: "a-clients", Count: 1},
	}
	input.States["0"] = initial

	next := exampleTimeline().States["0"]
	next.Resources.Backends = []scenario.BackendStateConfig{
		{ID: "z-new", Address: "10.0.0.3", Port: 80},
		{ID: "a-new", Address: "10.0.0.4", Port: 80},
	}
	next.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "z-burst", Count: 1, StartAt: 2}, {ID: "a-burst", Count: 1, StartAt: 2},
	}
	input.States["2"] = next

	plan := compileTimeline(t, input)
	change := plan.Transitions()[0]
	if !reflect.DeepEqual(change.Backends.Removed, []string{"a-old", "z-old"}) ||
		!reflect.DeepEqual(change.ClientGroups.Stopped, []string{"a-clients", "z-clients"}) {
		t.Fatalf("removed resources are not ordered by ID: %+v", change)
	}
	if len(change.Backends.Added) != 2 || change.Backends.Added[0].ID != "a-new" ||
		change.Backends.Added[1].ID != "z-new" {
		t.Fatalf("added backends are not ordered by ID: %+v", change.Backends.Added)
	}
	if len(change.ClientGroups.Added) != 2 || change.ClientGroups.Added[0].ID != "a-burst" ||
		change.ClientGroups.Added[1].ID != "z-burst" {
		t.Fatalf("added generators are not ordered by ID: %+v", change.ClientGroups.Added)
	}
	if plan.TotalClients() != 4 {
		t.Fatal("stopping a generator incorrectly released its historical client reservation")
	}

	// Retained updates must be ordered too, even when the next declaration starts with z.
	updated := next
	updated.Resources.Backends = append([]scenario.BackendStateConfig(nil), next.Resources.Backends...)
	updated.Workload.ClientGroups = append([]scenario.ClientGroup(nil), next.Workload.ClientGroups...)
	for index := range updated.Resources.Backends {
		updated.Resources.Backends[index].Name = "updated"
	}
	for index := range updated.Workload.ClientGroups {
		updated.Workload.ClientGroups[index].Stream.Bytes = pointer(100)
	}
	input.States["3"] = updated
	change = compileTimeline(t, input).Transitions()[1]
	if len(change.Backends.Updated) != 2 || change.Backends.Updated[0].Config.ID != "a-new" ||
		change.Backends.Updated[1].Config.ID != "z-new" {
		t.Fatalf("updated backends are not ordered by ID: %+v", change.Backends.Updated)
	}
	if len(change.ClientGroups.Updated) != 2 || change.ClientGroups.Updated[0].Config.ID != "a-burst" ||
		change.ClientGroups.Updated[1].Config.ID != "z-burst" {
		t.Fatalf("updated generators are not ordered by ID: %+v", change.ClientGroups.Updated)
	}
}

// A bad last state must reject compilation, without returning a valid prefix plan.
func TestCompileRejectsTheWholeInvalidTimeline(t *testing.T) {
	input := exampleTimeline()
	next := exampleTimeline().States["0"]
	next.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 0
	input.States["2"] = next

	plan, err := scenario.Compile(input)
	if err == nil || !strings.Contains(err.Error(), "ingress.bandwidthBitsPerSecond") || plan != nil {
		t.Fatalf("invalid timeline returned plan=%v, error=%v", plan, err)
	}
}
