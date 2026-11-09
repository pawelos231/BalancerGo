package scenario

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// NormalizeV2 checks the whole timeline before returning any usable configuration.
// Each state starts from fixed defaults; no world or per-client objects are created.
func NormalizeV2(input ScenarioV2) (NormalizedV2, error) {
	if err := checkDocumentV2(input); err != nil {
		return NormalizedV2{}, err
	}

	fixed, profiles, err := normalizeFixedV2(input.Fixed)
	if err != nil {
		return NormalizedV2{}, err
	}
	times, err := timelineTimes(input.States, fixed.Execution.Duration)
	if err != nil {
		return NormalizedV2{}, err
	}

	result := NormalizedV2{
		Version: VersionV2,
		Fixed:   fixed,
		States:  make([]NormalizedState, 0, len(times)),
	}
	identities := timelineIdentities{entities: make(map[string]*timelineIdentity)}

	// Numeric order lets identity checks notice removals between two declarations.
	for index, at := range times {
		key := strconv.FormatInt(int64(at), 10)
		state, err := normalizeStateV2(
			input.States[key],
			input.Fixed,
			profiles,
			&identities,
			at,
			index,
		)
		if err != nil {
			return NormalizedV2{}, err
		}
		result.States = append(result.States, state)
	}

	result.TotalClients = identities.reservedClients
	return result, nil
}

// normalizeFixedV2 resolves TCP and shared defaults once for the entire document.
func normalizeFixedV2(input FixedConfig) (NormalizedFixedConfig, map[string]LinkPatch, error) {
	if err := validateExecution(input.Execution); err != nil {
		return NormalizedFixedConfig{}, nil, fmt.Errorf("fixed.%w", err)
	}
	if input.Execution.Duration == 0 {
		return NormalizedFixedConfig{}, nil, fmt.Errorf("fixed.execution.durationNanoseconds: must be positive")
	}
	if err := validateID(input.LoadBalancer.ID, "fixed.loadBalancer.id"); err != nil {
		return NormalizedFixedConfig{}, nil, err
	}

	tcp, err := normalizeTCP(input.TCP)
	if err != nil {
		return NormalizedFixedConfig{}, nil, fmt.Errorf("fixed.%w", err)
	}
	if err := validateLink(input.Network.Defaults, "fixed.network.defaults"); err != nil {
		return NormalizedFixedConfig{}, nil, err
	}
	profiles, err := normalizeProfiles(input.Network)
	if err != nil {
		return NormalizedFixedConfig{}, nil, fmt.Errorf("fixed.%w", err)
	}
	stream, err := normalizeStream(input.Workload.Defaults, "fixed.workload.defaults")
	if err != nil {
		return NormalizedFixedConfig{}, nil, err
	}

	return NormalizedFixedConfig{
		Execution:       input.Execution,
		TCP:             tcp,
		LoadBalancer:    input.LoadBalancer,
		NetworkDefaults: input.Network.Defaults,
		DefaultStream:   stream,
	}, profiles, nil
}

// timelineTimes requires a state at zero and sorts timestamps as exact nanoseconds.
// Canonical spelling has already been checked by checkDocumentV2.
func timelineTimes(states map[string]StateConfig, duration time.Duration) ([]time.Duration, error) {
	if _, exists := states["0"]; !exists {
		return nil, fmt.Errorf("states: a state at timestamp 0 is required")
	}

	times := make([]time.Duration, 0, len(states))
	for key := range states {
		nanoseconds, _ := strconv.ParseInt(key, 10, 64)
		at := time.Duration(nanoseconds)
		if at >= duration {
			return nil, fmt.Errorf(
				"%s: timestamp must be below the simulation duration",
				documentMapPath("states", key),
			)
		}
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return times, nil
}

// normalizeStateV2 resolves a complete state and checks its identities against earlier states.
func normalizeStateV2(
	input StateConfig,
	fixed FixedConfig,
	profiles map[string]LinkPatch,
	identities *timelineIdentities,
	at time.Duration,
	stateIndex int,
) (NormalizedState, error) {
	path := documentMapPath("states", strconv.FormatInt(int64(at), 10))
	lbPath := path + ".resources.loadBalancer"
	loadBalancer, err := normalizeLoadBalancer(input.Resources.LoadBalancer, fixed.LoadBalancer.ID, fixed.Execution.Duration, lbPath)
	if err != nil {
		return NormalizedState{}, err
	}
	if len(input.Resources.Backends) > MaxBackends {
		return NormalizedState{}, fmt.Errorf("%s.resources.backends: count must not exceed %d", path, MaxBackends)
	}

	result := NormalizedState{
		At:           at,
		LoadBalancer: loadBalancer,
		Backends:     make([]NormalizedBackendState, 0, len(input.Resources.Backends)),
		ClientGroups: make([]NormalizedClientGroup, 0, len(input.Workload.ClientGroups)),
	}
	usedIDs := map[string]string{fixed.LoadBalancer.ID: "fixed.loadBalancer.id"}
	usedAddresses := make(map[string]string)
	defaultLinks := DirectionalLinks{
		ToLoadBalancer:   fixed.Network.Defaults,
		FromLoadBalancer: fixed.Network.Defaults,
	}

	// Empty backend lists are legal, for example during a complete outage.
	for index, backend := range input.Resources.Backends {
		backendPath := fmt.Sprintf("%s.resources.backends[%d]", path, index)
		if err := registerID(usedIDs, backend.ID, backendPath+".id"); err != nil {
			return NormalizedState{}, err
		}
		key := endpointKeyV2(backend.Address, backend.Port)
		if err := validateAddress(backend.Address, backend.Port, usedAddresses, backendPath, key); err != nil {
			return NormalizedState{}, err
		}
		if err := identities.backend(backend, backendPath, stateIndex); err != nil {
			return NormalizedState{}, err
		}

		resolved, err := normalizeBackendState(
			backend,
			fixed.Execution.Duration,
			defaultLinks,
			profiles,
			backendPath,
		)
		if err != nil {
			return NormalizedState{}, err
		}
		result.Backends = append(result.Backends, resolved)
	}

	// Preserve generator order and reserve historical slots without expanding clients.
	for index, group := range input.Workload.ClientGroups {
		groupPath := fmt.Sprintf("%s.workload.clientGroups[%d]", path, index)
		if err := registerID(usedIDs, group.ID, groupPath+".id"); err != nil {
			return NormalizedState{}, err
		}
		clientSlots, err := identities.group(group, at, groupPath, stateIndex)
		if err != nil {
			return NormalizedState{}, err
		}
		resolved, err := normalizeGroup(
			group,
			clientSlots,
			fixed.Workload.Defaults,
			defaultLinks,
			profiles,
			groupPath,
		)
		if err != nil {
			return NormalizedState{}, err
		}
		result.TotalClients += group.Count
		result.ClientGroups = append(result.ClientGroups, resolved)
	}
	return result, nil
}

// normalizeBackendState resolves links, CPU and availability afresh for this state.
func normalizeBackendState(
	input BackendStateConfig,
	duration time.Duration,
	links DirectionalLinks,
	profiles map[string]LinkPatch,
	path string,
) (NormalizedBackendState, error) {
	network, err := resolveNetwork(links, input.Network, profiles, path+".network")
	if err != nil {
		return NormalizedBackendState{}, err
	}
	host, err := normalizeHost(input.Host, duration, path+".host")
	if err != nil {
		return NormalizedBackendState{}, err
	}

	result := NormalizedBackendState{
		NormalizedBackend: NormalizedBackend{
			ID:      input.ID,
			Name:    input.Name,
			Address: input.Address,
			Port:    input.Port,
			Network: network,
			Host:    host,
		},
		PhysicalAvailable:   true,
		AdministrativeState: BackendAdminActive,
	}
	if input.PhysicalAvailable != nil {
		result.PhysicalAvailable = *input.PhysicalAvailable
	}
	if input.AdministrativeState != nil {
		result.AdministrativeState = *input.AdministrativeState
	}
	if result.AdministrativeState != BackendAdminActive && result.AdministrativeState != BackendAdminDrain {
		return NormalizedBackendState{}, fmt.Errorf("%s.administrativeState: must be active or drain", path)
	}
	return result, nil
}
