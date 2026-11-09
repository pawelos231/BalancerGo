package scenario

import "time"

// Compiled holds checked states and the changes to apply after the initial state.
// Its accessors return copies, so inspecting a plan cannot change later execution.
type Compiled struct {
	scenario    Normalized
	transitions []Transition
}

// Transition groups all changes at one virtual timestamp into one atomic update.
// Initial resources come from StateAt(0); this list starts after zero.
type Transition struct {
	At           time.Duration
	LoadBalancer *LoadBalancerUpdate // Nil means no LB settings changed.
	Backends     BackendChanges
	ClientGroups ClientGroupChanges
}

// LoadBalancerUpdate identifies which parts of the LB need new settings.
// For example, a timeout edit does not require reconfiguring its links or CPU.
type LoadBalancerUpdate struct {
	Config            NormalizedLoadBalancerConfig
	IngressChanged    bool
	ForwardingChanged bool
	HostChanged       bool
	AlgorithmChanged  bool
	TimeoutsChanged   bool
}

// BackendChanges compares membership by ID, regardless of list order in the document.
// Removal concerns that backend; it does not instantly terminate remote clients.
type BackendChanges struct {
	Added   []NormalizedBackendState
	Updated []BackendUpdate
	Removed []string
}

// BackendUpdate keeps unchanged links and CPU out of unrelated edits such as a new name.
type BackendUpdate struct {
	Config              NormalizedBackendState
	LinksChanged        bool
	HostChanged         bool
	NameChanged         bool
	AvailabilityChanged bool
	AdmissionChanged    bool
}

// ClientGroupChanges starts, updates or stops generators without deleting born clients.
// Stopped IDs keep their existing clients and their last link settings.
type ClientGroupChanges struct {
	Added   []NormalizedClientGroup
	Updated []ClientGroupUpdate
	Stopped []string
}

// ClientGroupUpdate separates birth timing, existing links and future stream settings.
// A stream edit never replaces the stream of a client that has already been born.
type ClientGroupUpdate struct {
	Config         NormalizedClientGroup
	ReplanArrivals bool // Only a count or client-gap change replans future births.
	LinksChanged   bool // Group links and resolved individual network overrides.
	StreamChanged  bool // Settings for future clients, including individual overrides.
}
