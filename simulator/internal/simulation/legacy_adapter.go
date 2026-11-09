package simulation

import "load-balancer/scenario"

// New is the temporary v1 boundary for callers being migrated to NewTimeline.
// The world stores native fixed/state data, including the original zero horizon.
func New(resolved scenario.Normalized) *World {
	backends := make([]scenario.NormalizedBackendState, 0, len(resolved.Backends))
	for _, backend := range resolved.Backends {
		backends = append(backends, scenario.NormalizedBackendState{
			NormalizedBackend: backend, PhysicalAvailable: true,
			AdministrativeState: scenario.BackendAdminActive,
		})
	}
	fixed := scenario.NormalizedFixedConfig{
		Execution: resolved.Execution, TCP: resolved.TCP,
		LoadBalancer:    scenario.LoadBalancerIdentity{ID: resolved.LoadBalancer.ID},
		NetworkDefaults: resolved.NetworkDefaults, DefaultStream: resolved.DefaultStream,
	}
	initial := scenario.NormalizedState{
		LoadBalancer: resolved.LoadBalancer, Backends: backends,
		ClientGroups: resolved.ClientGroups, TotalClients: resolved.TotalClients,
	}
	return newWorld(fixed, initial, nil, nil)
}
