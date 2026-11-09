package scenario

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// timelineIdentity remembers immutable fields and client reservations after removal.
// lastState detects a missing entity even if it returns with the same settings later.
type timelineIdentity struct {
	role        string
	firstPath   string
	lastState   int
	endpoint    string
	startAt     time.Duration
	clientSlots int
}

// timelineIdentities checks identities across states, rather than just within one state.
type timelineIdentities struct {
	entities        map[string]*timelineIdentity
	reservedClients int
}

// visit rejects role changes and reappearance after removal; consecutive visits retain identity.
func (identities *timelineIdentities) visit(
	id, role, path string,
	stateIndex int,
) (*timelineIdentity, bool, error) {
	previous, exists := identities.entities[id]
	if !exists {
		identity := &timelineIdentity{role: role, firstPath: path, lastState: stateIndex}
		identities.entities[id] = identity
		return identity, true, nil
	}

	if previous.role != role {
		return nil, false, fmt.Errorf("%s.id: id %q already belongs to %s", path, id, previous.firstPath)
	}
	if previous.lastState != stateIndex-1 {
		return nil, false, fmt.Errorf("%s.id: id %q cannot be reused after removal", path, id)
	}

	previous.lastState = stateIndex
	return previous, false, nil
}

// backend keeps the same endpoint for a retained ID, allowing a display-name change.
func (identities *timelineIdentities) backend(
	backend BackendStateConfig,
	path string,
	stateIndex int,
) error {
	identity, firstVisit, err := identities.visit(backend.ID, "backend", path, stateIndex)
	if err != nil {
		return err
	}

	endpoint := endpointKey(backend.Address, backend.Port)
	if !firstVisit && identity.endpoint != endpoint {
		return fmt.Errorf("%s: retained backend address and port must not change", path)
	}
	identity.endpoint = endpoint
	return nil
}

// group reserves the largest declared budget, even when later states reduce it.
// For count 10 -> 1, overrides may still configure any of the original ten slots.
func (identities *timelineIdentities) group(
	group ClientGroup,
	at time.Duration,
	path string,
	stateIndex int,
) (int, error) {
	identity, firstVisit, err := identities.visit(group.ID, "client group", path, stateIndex)
	if err != nil {
		return 0, err
	}

	if firstVisit {
		if group.StartAt < at {
			return 0, fmt.Errorf("%s.startAtNanoseconds: new group must start at or after its insertion time", path)
		}
		identity.startAt = group.StartAt
	} else if group.StartAt != identity.startAt {
		return 0, fmt.Errorf("%s.startAtNanoseconds: retained group start time must not change", path)
	}

	if group.Count < 0 {
		return 0, fmt.Errorf("%s.count: must be nonnegative", path)
	}

	// Removed groups and smaller budgets do not release previously reserved addresses.
	additionalSlots := max(group.Count-identity.clientSlots, 0)
	if additionalSlots > MaxClients-identities.reservedClients {
		return 0, fmt.Errorf("%s.count: total reserved clients must not exceed %d", path, MaxClients)
	}
	identities.reservedClients += additionalSlots
	identity.clientSlots += additionalSlots
	return identity.clientSlots, nil
}

// endpointKey folds only ASCII letters so hostname identity does not depend on Unicode versions.
// Other characters remain literal; equivalent IPv6 spellings still identify the same IP.
func endpointKey(address string, port uint16) string {
	bytes := []byte(address)
	for index, character := range bytes {
		if character >= 'A' && character <= 'Z' {
			bytes[index] += 'a' - 'A'
		}
	}
	host := string(bytes)
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}
