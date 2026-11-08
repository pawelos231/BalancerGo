package scenario

import "time"

const VersionV2 = 2

// ScenarioV2 separates run-wide settings from configurations selected by virtual time.
// State keys are decimal nanoseconds; decoding alone does not make the document runnable.
type ScenarioV2 struct {
	Version int                    `json:"version"`
	Fixed   FixedConfig            `json:"fixed"`
	States  map[string]StateConfig `json:"states"`
}

// FixedConfig stays unchanged for the whole run, including TCP and shared defaults.
// Each state resolves its own settings from here, without inheriting the previous state.
type FixedConfig struct {
	Execution    ExecutionConfig        `json:"execution"`
	TCP          TCPConfig              `json:"tcp"`
	LoadBalancer LoadBalancerIdentity   `json:"loadBalancer"`
	Network      NetworkConfig          `json:"network"`
	Workload     WorkloadDefaultsConfig `json:"workload"`
}

// LoadBalancerIdentity keeps the balancer's ID out of the settings that change over time.
type LoadBalancerIdentity struct {
	ID string `json:"id"`
}

// WorkloadDefaultsConfig supplies stream defaults to every state's client groups.
type WorkloadDefaultsConfig struct {
	Defaults StreamConfig `json:"defaults"`
}

// StateConfig is the complete configuration from its timestamp until the next state.
// It describes resources and future client arrivals, not live connections or queue contents.
type StateConfig struct {
	Resources StateResourcesConfig `json:"resources"`
	Workload  StateWorkloadConfig  `json:"workload"`
}

// StateResourcesConfig declares the LB settings and all backends present in this state.
// Leaving a backend out means removing it when the state is applied.
type StateResourcesConfig struct {
	LoadBalancer LoadBalancerStateConfig `json:"loadBalancer"`
	Backends     []BackendStateConfig    `json:"backends"`
}

// LoadBalancerStateConfig contains settings that may change without replacing the LB.
// Algorithm changes affect new mappings; timeout changes also apply to existing mappings.
type LoadBalancerStateConfig struct {
	Algorithm               string                    `json:"algorithm"`
	Host                    HostConfig                `json:"host,omitempty"`
	Ingress                 LinkConfig                `json:"ingress"`
	ForwardingBitsPerSecond int64                     `json:"forwardingBitsPerSecond"`
	MappingTimeouts         LoadBalancerTimeoutConfig `json:"mappingTimeouts"`
}

// LoadBalancerTimeoutConfig groups idle limits for the LB's connection mappings.
// Nil selects a default; an explicit zero disables cleanup for that connection state.
type LoadBalancerTimeoutConfig struct {
	HalfOpenIdleTimeout    *time.Duration `json:"halfOpenIdleTimeoutNanoseconds,omitempty"`
	EstablishedIdleTimeout *time.Duration `json:"establishedIdleTimeoutNanoseconds,omitempty"`
	ClosingIdleTimeout     *time.Duration `json:"closingIdleTimeoutNanoseconds,omitempty"`
	ResetIdleTimeout       *time.Duration `json:"resetIdleTimeoutNanoseconds,omitempty"`
}

// BackendAdministrativeState is the operator's policy for admitting new connections.
// Drain keeps existing mappings while excluding this backend from new selections.
type BackendAdministrativeState string

const (
	BackendAdminActive BackendAdministrativeState = "active"
	BackendAdminDrain  BackendAdministrativeState = "drain"
)

// BackendStateConfig separates physical availability from health observed by the LB.
// Nil availability/admin resolve to true/active in each state, without carrying past values.
type BackendStateConfig struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name,omitempty"`
	Address             string                      `json:"address"`
	Port                uint16                      `json:"port"`
	Network             EndpointNetworkConfig       `json:"network"`
	Host                HostConfig                  `json:"host,omitempty"`
	PhysicalAvailable   *bool                       `json:"physicalAvailable,omitempty"`
	AdministrativeState *BackendAdministrativeState `json:"administrativeState,omitempty"`
}

// StateWorkloadConfig lists generators present in this state; existing clients keep running.
// Group count is a lifetime birth budget, not a target number of active connections.
type StateWorkloadConfig struct {
	ClientGroups []ClientGroup `json:"clientGroups"`
}
