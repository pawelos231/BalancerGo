package scenario

import (
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"

	"load-balancer/model"
)

// Normalize checks an input scenario and resolves its inherited settings.
// The result owns its data; client groups stay compact.
func Normalize(input Scenario) (Normalized, error) {
	// Check shared settings before resolving individual backends and clients.
	if input.Version != Version {
		return Normalized{}, fmt.Errorf("version: unsupported version %d", input.Version)
	}

	if err := validateExecution(input.Execution); err != nil {
		return Normalized{}, err
	}

	if err := validateLink(input.Network.Defaults, "network.defaults"); err != nil {
		return Normalized{}, err
	}

	profiles, err := normalizeProfiles(input.Network)
	if err != nil {
		return Normalized{}, err
	}

	defaultStream, err := normalizeStream(input.Workload.Defaults, "workload.defaults")
	if err != nil {
		return Normalized{}, err
	}

	if err := validateLoadBalancer(input.Resources.LoadBalancer); err != nil {
		return Normalized{}, err
	}

	backendCount := len(input.Resources.Backends)
	if backendCount > MaxBackends {
		return Normalized{}, fmt.Errorf(
			"resources.backends: count must not exceed %d",
			MaxBackends,
		)
	}

	// Prepare independent output slices and registries for duplicate checks.
	groupCount := len(input.Workload.ClientGroups)
	result := Normalized{
		Version:         Version,
		Execution:       input.Execution,
		NetworkDefaults: input.Network.Defaults,
		LoadBalancer:    input.Resources.LoadBalancer,
		Backends:        make([]NormalizedBackend, 0, backendCount),
		DefaultStream:   defaultStream,
		ClientGroups:    make([]NormalizedClientGroup, 0, groupCount),
	}

	usedIDs := map[string]string{
		input.Resources.LoadBalancer.ID: "resources.loadBalancer.id",
	}
	usedAddresses := make(map[string]string)

	defaultLinks := DirectionalLinks{
		ToLoadBalancer:   input.Network.Defaults,
		FromLoadBalancer: input.Network.Defaults,
	}

	// Resolve each backend's links while keeping the original backend order.
	for index, backend := range input.Resources.Backends {
		path := fmt.Sprintf("resources.backends[%d]", index)

		if err := registerID(usedIDs, backend.ID, path+".id"); err != nil {
			return Normalized{}, err
		}

		if err := validateAddress(backend, usedAddresses, path); err != nil {
			return Normalized{}, err
		}

		networkPath := path + ".network"
		links, err := resolveNetwork(defaultLinks, backend.Network, profiles, networkPath)
		if err != nil {
			return Normalized{}, err
		}

		result.Backends = append(result.Backends, NormalizedBackend{
			ID:      backend.ID,
			Name:    backend.Name,
			Address: backend.Address,
			Port:    backend.Port,
			Network: links,
		})
	}

	// Resolve shared group settings without allocating one entry per client.
	for index, group := range input.Workload.ClientGroups {
		path := fmt.Sprintf("workload.clientGroups[%d]", index)

		if err := registerID(usedIDs, group.ID, path+".id"); err != nil {
			return Normalized{}, err
		}

		remainingClients := MaxClients - result.TotalClients
		if group.Count < 0 || group.Count > remainingClients {
			return Normalized{}, fmt.Errorf(
				"%s.count: total clients must be in 0..%d",
				path,
				MaxClients,
			)
		}

		resolvedGroup, err := normalizeGroup(
			group,
			input.Workload.Defaults,
			defaultLinks,
			profiles,
			path,
		)
		if err != nil {
			return Normalized{}, err
		}

		result.TotalClients += group.Count
		result.ClientGroups = append(result.ClientGroups, resolvedGroup)
	}

	return result, nil
}

// validateExecution checks run limits, snapshot timing and real-time pacing.
func validateExecution(config ExecutionConfig) error {
	if config.Duration < 0 {
		return fmt.Errorf("execution.durationNanoseconds: must be nonnegative")
	}

	if config.SnapshotInterval <= 0 {
		return fmt.Errorf("execution.snapshotIntervalNanoseconds: must be positive")
	}

	invalidSpeed := config.Speed < 0 ||
		math.IsNaN(config.Speed) ||
		math.IsInf(config.Speed, 0)

	if invalidSpeed {
		return fmt.Errorf("execution.speed: must be finite and nonnegative")
	}

	if config.MaxEvents == 0 {
		return fmt.Errorf("execution.maxEvents: must be positive")
	}

	// Speed zero disables pacing, so no real-time duration needs to be computed.
	if config.Speed > 0 {
		pacedDuration := float64(config.Duration) / config.Speed
		if pacedDuration >= float64(math.MaxInt64) {
			return fmt.Errorf("execution: paced duration exceeds time.Duration range")
		}
	}

	return nil
}

// validateLoadBalancer checks the balancer ID, selection algorithm and output limits.
func validateLoadBalancer(config LoadBalancerConfig) error {
	if err := validateID(config.ID, "resources.loadBalancer.id"); err != nil {
		return err
	}

	supportedAlgorithm := config.Algorithm == "roundRobin" || config.Algorithm == "random"
	if !supportedAlgorithm {
		return fmt.Errorf("resources.loadBalancer.algorithm: must be roundRobin or random")
	}

	if config.ForwardingBitsPerSecond <= 0 {
		return fmt.Errorf("resources.loadBalancer.forwardingBitsPerSecond: must be positive")
	}

	if config.QueueCapacity < 0 {
		return fmt.Errorf("resources.loadBalancer.queueCapacity: must be nonnegative")
	}

	return nil
}

// validateLink checks one direction's speed, travel time, queue size and loss.
func validateLink(config LinkConfig, path string) error {
	if config.BandwidthBitsPerSecond <= 0 {
		return fmt.Errorf("%s.bandwidthBitsPerSecond: must be positive", path)
	}

	if config.PacketTravelTime < 0 {
		return fmt.Errorf("%s.packetTravelTimeNanoseconds: must be nonnegative", path)
	}

	if config.QueueCapacity < 0 {
		return fmt.Errorf("%s.queueCapacity: must be nonnegative", path)
	}

	lossOutOfRange := config.LossRate < 0 || config.LossRate > 1
	lossNotFinite := math.IsNaN(config.LossRate) || math.IsInf(config.LossRate, 0)

	if lossOutOfRange || lossNotFinite {
		return fmt.Errorf("%s.lossRate: must be finite and in 0..1", path)
	}

	return nil
}

// normalizeProfiles checks named link profiles and indexes them by ID.
// It keeps raw patches so unspecified fields can still inherit endpoint settings.
func normalizeProfiles(config NetworkConfig) (map[string]LinkPatch, error) {
	profiles := make(map[string]LinkPatch, len(config.Profiles))

	for index, profile := range config.Profiles {
		path := fmt.Sprintf("network.profiles[%d]", index)

		if err := validateID(profile.ID, path+".id"); err != nil {
			return nil, err
		}

		if _, exists := profiles[profile.ID]; exists {
			return nil, fmt.Errorf("%s.id: duplicate profile %q", path, profile.ID)
		}

		// Check the effective values, but store only the fields the profile supplies.
		resolved := applyLinkPatch(config.Defaults, profile.Config)
		if err := validateLink(resolved, path+".config"); err != nil {
			return nil, err
		}

		profiles[profile.ID] = profile.Config
	}

	return profiles, nil
}

// resolveNetwork applies a profile, then directional overrides to inherited links.
// Both resulting directions are validated before they are returned.
func resolveNetwork(
	base DirectionalLinks,
	config EndpointNetworkConfig,
	profiles map[string]LinkPatch,
	path string,
) (DirectionalLinks, error) {
	// A selected profile applies to both directions.
	if config.Profile != "" {
		profile, exists := profiles[config.Profile]
		if !exists {
			return DirectionalLinks{}, fmt.Errorf(
				"%s.profile: unknown profile %q",
				path,
				config.Profile,
			)
		}

		base.ToLoadBalancer = applyLinkPatch(base.ToLoadBalancer, profile)
		base.FromLoadBalancer = applyLinkPatch(base.FromLoadBalancer, profile)
	}

	// Direction-specific settings take precedence over the selected profile.
	base.ToLoadBalancer = applyLinkPatch(base.ToLoadBalancer, config.ToLoadBalancer)
	base.FromLoadBalancer = applyLinkPatch(base.FromLoadBalancer, config.FromLoadBalancer)

	if err := validateLink(base.ToLoadBalancer, path+".toLoadBalancer"); err != nil {
		return DirectionalLinks{}, err
	}

	if err := validateLink(base.FromLoadBalancer, path+".fromLoadBalancer"); err != nil {
		return DirectionalLinks{}, err
	}

	return base, nil
}

// applyLinkPatch copies provided settings onto an existing link configuration.
// Nil fields leave inherited values in place.
func applyLinkPatch(config LinkConfig, patch LinkPatch) LinkConfig {
	if patch.BandwidthBitsPerSecond != nil {
		config.BandwidthBitsPerSecond = *patch.BandwidthBitsPerSecond
	}

	if patch.PacketTravelTime != nil {
		config.PacketTravelTime = *patch.PacketTravelTime
	}

	if patch.QueueCapacity != nil {
		config.QueueCapacity = *patch.QueueCapacity
	}

	if patch.LossRate != nil {
		config.LossRate = *patch.LossRate
	}

	return config
}

// normalizeStream resolves bytes=-1 and checks stream size and timing limits.
func normalizeStream(config StreamConfig, path string) (StreamConfig, error) {
	if config.Packets < 0 {
		return StreamConfig{}, fmt.Errorf("%s.packets: must be nonnegative", path)
	}

	if config.Bytes < -1 {
		return StreamConfig{}, fmt.Errorf("%s.bytes: must be -1 or nonnegative", path)
	}

	if config.PacketGap < 0 {
		return StreamConfig{}, fmt.Errorf("%s.packetGapNanoseconds: must be nonnegative", path)
	}

	// Derive payload bytes only after all relevant overrides have been applied.
	if config.Bytes == -1 {
		maxPacketCount := math.MaxInt / model.SEGMENT_SIZE
		if config.Packets > maxPacketCount {
			return StreamConfig{}, fmt.Errorf("%s.packets: stream size overflows int", path)
		}

		config.Bytes = config.Packets * model.SEGMENT_SIZE
	}

	segmentCount := uint64(config.Bytes / model.SEGMENT_SIZE)
	segmentLimitExceeded := segmentCount >= math.MaxUint32
	byteLimitExceeded := config.Bytes > math.MaxInt-40

	if segmentLimitExceeded || byteLimitExceeded {
		return StreamConfig{}, fmt.Errorf(
			"%s.bytes: stream exceeds the simplified transport's range",
			path,
		)
	}

	return config, nil
}

// applyStreamPatch copies provided stream settings onto inherited values.
func applyStreamPatch(config StreamConfig, patch StreamPatch) StreamConfig {
	if patch.Packets != nil {
		config.Packets = *patch.Packets
	}

	if patch.Bytes != nil {
		config.Bytes = *patch.Bytes
	}

	if patch.PacketGap != nil {
		config.PacketGap = *patch.PacketGap
	}

	return config
}

// normalizeGroup resolves shared group settings and individual client exceptions.
// It stores only those exceptions instead of creating every client.
func normalizeGroup(
	group ClientGroup,
	defaults StreamConfig,
	links DirectionalLinks,
	profiles map[string]LinkPatch,
	path string,
) (NormalizedClientGroup, error) {
	if group.StartAt < 0 {
		return NormalizedClientGroup{}, fmt.Errorf("%s.startAtNanoseconds: must be nonnegative", path)
	}

	if group.ClientGap < 0 {
		return NormalizedClientGroup{}, fmt.Errorf("%s.clientGapNanoseconds: must be nonnegative", path)
	}

	network, err := resolveNetwork(links, group.Network, profiles, path+".network")
	if err != nil {
		return NormalizedClientGroup{}, err
	}

	// Keep bytes=-1 in the raw group stream until client overrides are applied.
	groupStream := applyStreamPatch(defaults, group.Stream)
	stream, err := normalizeStream(groupStream, path+".stream")
	if err != nil {
		return NormalizedClientGroup{}, err
	}

	result := NormalizedClientGroup{
		ID:        group.ID,
		Count:     group.Count,
		StartAt:   group.StartAt,
		ClientGap: group.ClientGap,
		Network:   network,
		Stream:    stream,
		Overrides: make([]NormalizedClientOverride, 0, len(group.Overrides)),
	}

	// Each client index must exist in the group and occur only once.
	usedIndices := make(map[int]bool, len(group.Overrides))

	for position, override := range group.Overrides {
		overridePath := fmt.Sprintf("%s.overrides[%d]", path, position)

		if override.Index < 0 || override.Index >= group.Count {
			return NormalizedClientGroup{}, fmt.Errorf(
				"%s.index: must identify a client in this group",
				overridePath,
			)
		}

		if usedIndices[override.Index] {
			return NormalizedClientGroup{}, fmt.Errorf(
				"%s.index: duplicate client index %d",
				overridePath,
				override.Index,
			)
		}

		usedIndices[override.Index] = true

		clientLinks, err := resolveNetwork(
			network,
			override.Network,
			profiles,
			overridePath+".network",
		)
		if err != nil {
			return NormalizedClientGroup{}, err
		}

		clientStreamInput := applyStreamPatch(groupStream, override.Stream)
		clientStream, err := normalizeStream(clientStreamInput, overridePath+".stream")
		if err != nil {
			return NormalizedClientGroup{}, err
		}

		result.Overrides = append(result.Overrides, NormalizedClientOverride{
			Index:   override.Index,
			Network: clientLinks,
			Stream:  clientStream,
		})
	}

	// Sorting the new slice enables lookup without changing the input order.
	sort.Slice(result.Overrides, func(i, j int) bool {
		return result.Overrides[i].Index < result.Overrides[j].Index
	})

	return result, nil
}

// Client returns one client's resolved settings without creating a runtime client.
// It uses group settings unless this index has an individual override.
func (group NormalizedClientGroup) Client(index int) (ClientConfig, error) {
	if index < 0 || index >= group.Count {
		return ClientConfig{}, fmt.Errorf("client index %d outside group %q", index, group.ID)
	}

	result := ClientConfig{
		ID:      "client/" + group.ID + "/" + strconv.Itoa(index),
		Network: group.Network,
		Stream:  group.Stream,
	}

	// Find the first override at or after this index.
	position := sort.Search(len(group.Overrides), func(searchPosition int) bool {
		return group.Overrides[searchPosition].Index >= index
	})

	if position < len(group.Overrides) {
		override := group.Overrides[position]

		if override.Index == index {
			result.Network = override.Network
			result.Stream = override.Stream
		}
	}

	return result, nil
}

// validateID checks ID length and the supported ASCII characters.
func validateID(id, path string) error {
	if id == "" || len(id) > 128 {
		return fmt.Errorf("%s: id length must be in 1..128 bytes", path)
	}

	for _, character := range id {
		lowercaseLetter := character >= 'a' && character <= 'z'
		uppercaseLetter := character >= 'A' && character <= 'Z'
		letter := lowercaseLetter || uppercaseLetter
		digit := character >= '0' && character <= '9'
		separator := character == '-' || character == '_' || character == '.'

		if !letter && !digit && !separator {
			return fmt.Errorf(
				"%s: id must contain only ASCII letters, digits, '-', '_' or '.'",
				path,
			)
		}
	}

	return nil
}

// registerID records an ID and rejects reuse by another configured object.
func registerID(ids map[string]string, id, path string) error {
	if err := validateID(id, path); err != nil {
		return err
	}

	if previous, exists := ids[id]; exists {
		return fmt.Errorf("%s: id %q already used by %s", path, id, previous)
	}

	ids[id] = path
	return nil
}

// validateAddress checks the host and port, then rejects duplicate endpoints.
// Host casing and equivalent IP spellings use the same comparison key.
func validateAddress(backend BackendConfig, addresses map[string]string, path string) error {
	emptyHost := backend.Address == ""
	hostHasWhitespace := strings.ContainsAny(backend.Address, " \t\r\n")

	if emptyHost || hostHasWhitespace {
		return fmt.Errorf("%s.address: must be a nonempty host without whitespace", path)
	}

	if backend.Port == 0 {
		return fmt.Errorf("%s.port: must be in 1..65535", path)
	}

	// Normalize only the comparison key; keep the original address in the result.
	host := strings.ToLower(backend.Address)
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}

	address := net.JoinHostPort(host, strconv.Itoa(int(backend.Port)))
	if previous, exists := addresses[address]; exists {
		return fmt.Errorf(
			"%s.address: endpoint %q already used by %s",
			path,
			address,
			previous,
		)
	}

	addresses[address] = path
	return nil
}
