package scenario

import "sort"

// compileTransition compares retained IDs rather than their positions in the document.
// The input is already normalized, so immutable identity fields have been checked.
func compileTransition(previous, next NormalizedState) Transition {
	return Transition{
		At:           next.At,
		LoadBalancer: compareLoadBalancer(previous.LoadBalancer, next.LoadBalancer),
		Backends:     compareBackends(previous.Backends, next.Backends),
		ClientGroups: compareClientGroups(previous.ClientGroups, next.ClientGroups),
	}
}

// hasChanges keeps unchanged states available for inspection without scheduling empty work.
func hasChanges(transition Transition) bool {
	backendChanges := len(transition.Backends.Added) +
		len(transition.Backends.Updated) + len(transition.Backends.Removed)
	groupChanges := len(transition.ClientGroups.Added) +
		len(transition.ClientGroups.Updated) + len(transition.ClientGroups.Stopped)
	return transition.LoadBalancer != nil || backendChanges != 0 || groupChanges != 0
}

// compareLoadBalancer separates edits that need different runtime handlers later.
func compareLoadBalancer(previous, next NormalizedLoadBalancerConfig) *LoadBalancerUpdate {
	update := LoadBalancerUpdate{
		Config:            next,
		IngressChanged:    previous.Ingress != next.Ingress,
		ForwardingChanged: previous.ForwardingBitsPerSecond != next.ForwardingBitsPerSecond,
		HostChanged:       previous.Host != next.Host,
		AlgorithmChanged:  previous.Algorithm != next.Algorithm,
		TimeoutsChanged: previous.HalfOpenIdleTimeout != next.HalfOpenIdleTimeout ||
			previous.EstablishedIdleTimeout != next.EstablishedIdleTimeout ||
			previous.ClosingIdleTimeout != next.ClosingIdleTimeout ||
			previous.ResetIdleTimeout != next.ResetIdleTimeout,
	}

	changed := update.IngressChanged || update.ForwardingChanged || update.HostChanged ||
		update.AlgorithmChanged || update.TimeoutsChanged
	if !changed {
		return nil
	}
	return &update
}

// compareBackends classifies additions, updates and removals without touching live backends.
func compareBackends(previous, next []NormalizedBackendState) BackendChanges {
	remaining := make(map[string]NormalizedBackendState, len(previous))
	for _, backend := range previous {
		remaining[backend.ID] = backend
	}

	var changes BackendChanges
	for _, backend := range next {
		old, retained := remaining[backend.ID]
		if !retained {
			changes.Added = append(changes.Added, backend)
			continue
		}
		delete(remaining, backend.ID)

		// Canonical endpoint spelling is identity, not a mutable backend setting.
		update := BackendUpdate{
			Config:              backend,
			LinksChanged:        old.Network != backend.Network,
			HostChanged:         old.Host != backend.Host,
			NameChanged:         old.Name != backend.Name,
			AvailabilityChanged: old.PhysicalAvailable != backend.PhysicalAvailable,
			AdmissionChanged:    old.AdministrativeState != backend.AdministrativeState,
		}
		changed := update.LinksChanged || update.HostChanged || update.NameChanged ||
			update.AvailabilityChanged || update.AdmissionChanged
		if changed {
			changes.Updated = append(changes.Updated, update)
		}
	}
	for id := range remaining {
		changes.Removed = append(changes.Removed, id)
	}

	// Stable operation order must not depend on map iteration or declaration order.
	sort.Slice(changes.Added, func(i, j int) bool {
		return changes.Added[i].ID < changes.Added[j].ID
	})
	sort.Slice(changes.Updated, func(i, j int) bool {
		return changes.Updated[i].Config.ID < changes.Updated[j].Config.ID
	})
	sort.Strings(changes.Removed)
	return changes
}

// compareClientGroups stops omitted generators while preserving already born clients.
func compareClientGroups(previous, next []NormalizedClientGroup) ClientGroupChanges {
	remaining := make(map[string]NormalizedClientGroup, len(previous))
	for _, group := range previous {
		remaining[group.ID] = group
	}

	var changes ClientGroupChanges
	for _, group := range next {
		old, retained := remaining[group.ID]
		if !retained {
			changes.Added = append(changes.Added, group)
			continue
		}
		delete(remaining, group.ID)

		update := compareClientGroup(old, group)
		if update.ReplanArrivals || update.LinksChanged || update.StreamChanged {
			changes.Updated = append(changes.Updated, update)
		}
	}
	for id := range remaining {
		changes.Stopped = append(changes.Stopped, id)
	}

	sort.Slice(changes.Added, func(i, j int) bool {
		return changes.Added[i].ID < changes.Added[j].ID
	})
	sort.Slice(changes.Updated, func(i, j int) bool {
		return changes.Updated[i].Config.ID < changes.Updated[j].Config.ID
	})
	sort.Strings(changes.Stopped)
	return changes
}

// compareClientGroup distinguishes new arrival timing from link and stream edits.
// Sparse overrides are merged by index; millions of ordinary clients need no entries here.
func compareClientGroup(previous, next NormalizedClientGroup) ClientGroupUpdate {
	update := ClientGroupUpdate{
		Config:         next,
		ReplanArrivals: previous.Count != next.Count || previous.ClientGap != next.ClientGap,
		LinksChanged:   previous.Network != next.Network,
		StreamChanged:  previous.Stream != next.Stream,
	}

	previousIndex, nextIndex := 0, 0
	for previousIndex < len(previous.Overrides) || nextIndex < len(next.Overrides) {
		// Once both kinds of change are known, further overrides cannot change the result.
		if update.LinksChanged && update.StreamChanged {
			break
		}

		// Visit the next index present in either sorted override list.
		clientIndex := 0
		if previousIndex < len(previous.Overrides) {
			clientIndex = previous.Overrides[previousIndex].Index
		}
		if nextIndex < len(next.Overrides) &&
			(previousIndex == len(previous.Overrides) || next.Overrides[nextIndex].Index < clientIndex) {
			clientIndex = next.Overrides[nextIndex].Index
		}

		// Missing overrides use that state's group settings, not the other state's override.
		previousNetwork, previousStream := previous.Network, previous.Stream
		if previousIndex < len(previous.Overrides) && previous.Overrides[previousIndex].Index == clientIndex {
			override := previous.Overrides[previousIndex]
			previousNetwork, previousStream = override.Network, override.Stream
			previousIndex++
		}

		nextNetwork, nextStream := next.Network, next.Stream
		if nextIndex < len(next.Overrides) && next.Overrides[nextIndex].Index == clientIndex {
			override := next.Overrides[nextIndex]
			nextNetwork, nextStream = override.Network, override.Stream
			nextIndex++
		}

		update.LinksChanged = update.LinksChanged || previousNetwork != nextNetwork
		update.StreamChanged = update.StreamChanged || previousStream != nextStream
	}
	return update
}
