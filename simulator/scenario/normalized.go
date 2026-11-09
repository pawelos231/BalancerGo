package scenario

import "time"

// Normalized is a checked timeline with independent, fully resolved states.
// It owns its slices and keeps client groups compact, without creating runtime objects.
type Normalized struct {
	ScenarioVersion int                   `json:"scenarioVersion"`
	Fixed           NormalizedFixedConfig `json:"fixed"`
	States          []NormalizedState     `json:"states"`

	// Reserve each group's largest declared count, including groups later removed.
	TotalClients int `json:"totalClients"`
}

// NormalizedFixedConfig holds settings shared by the entire checked timeline.
// Profiles have been applied to each state's links and are no longer needed here.
type NormalizedFixedConfig struct {
	Execution       ExecutionConfig      `json:"execution"`
	TCP             NormalizedTCPConfig  `json:"tcp"`
	LoadBalancer    LoadBalancerIdentity `json:"loadBalancer"`
	NetworkDefaults LinkConfig           `json:"networkDefaults"`
	DefaultStream   StreamConfig         `json:"defaultStream"`
}

// NormalizedState is a full configuration at At, sorted by virtual time.
// It contains settings, not queued packets, TCP state or compiled transition actions.
type NormalizedState struct {
	At           time.Duration                `json:"atNanoseconds"`
	LoadBalancer NormalizedLoadBalancerConfig `json:"loadBalancer"`
	Backends     []NormalizedBackendState     `json:"backends"`
	ClientGroups []NormalizedClientGroup      `json:"clientGroups"`

	// Sum of this state's birth budgets, not a count of active connections.
	TotalClients int `json:"totalClients"`
}

// NormalizedBackendState adds concrete availability and admission policy to a backend.
// These describe the desired state; they do not report health observed by the LB.
type NormalizedBackendState struct {
	NormalizedBackend
	PhysicalAvailable   bool                       `json:"physicalAvailable"`
	AdministrativeState BackendAdministrativeState `json:"administrativeState"`
}
