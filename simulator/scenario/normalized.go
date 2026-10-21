package scenario

import "time"

// Normalized is the checked configuration with profiles and overrides resolved.
// It keeps compact client groups instead of allocating all clients upfront.
type Normalized struct {
	Version         int
	Execution       ExecutionConfig
	NetworkDefaults LinkConfig
	LoadBalancer    LoadBalancerConfig
	Backends        []NormalizedBackend
	DefaultStream   StreamConfig
	ClientGroups    []NormalizedClientGroup
	TotalClients    int
}

// DirectionalLinks holds the final settings for an endpoint's two link directions.
// ToLoadBalancer is client/backend -> LB; FromLoadBalancer is LB -> client/backend.
type DirectionalLinks struct {
	ToLoadBalancer   LinkConfig
	FromLoadBalancer LinkConfig
}

// NormalizedBackend is produced from BackendConfig after validation.
// Its settings already include defaults, the selected profile and overrides.
type NormalizedBackend struct {
	ID      string
	Name    string
	Address string
	Port    uint16
	Network DirectionalLinks
}

// NormalizedClientGroup holds shared settings and resolved client exceptions.
// Client(index) retrieves one client's settings without expanding the whole group.
type NormalizedClientGroup struct {
	ID        string
	Count     int
	StartAt   time.Duration
	ClientGap time.Duration
	Network   DirectionalLinks
	Stream    StreamConfig
	Overrides []NormalizedClientOverride
}

// NormalizedClientOverride holds the final settings of one client with overrides.
// Client(index) uses these settings when the requested index matches.
type NormalizedClientOverride struct {
	Index   int
	Network DirectionalLinks
	Stream  StreamConfig
}

// ClientConfig contains one client's resolved link and stream settings.
// Client(index) returns it with a stable ID derived from the group and index.
type ClientConfig struct {
	ID      string
	Network DirectionalLinks
	Stream  StreamConfig
}
