package scenario

import "time"

// Normalized is the checked configuration with profiles and overrides resolved.
// It keeps compact client groups instead of allocating all clients upfront.
type Normalized struct {
	Version         int                          `json:"version"`
	Execution       ExecutionConfig              `json:"execution"`
	TCP             NormalizedTCPConfig          `json:"tcp"`
	NetworkDefaults LinkConfig                   `json:"networkDefaults"`
	LoadBalancer    NormalizedLoadBalancerConfig `json:"loadBalancer"`
	Backends        []NormalizedBackend          `json:"backends"`
	DefaultStream   StreamConfig                 `json:"defaultStream"`
	ClientGroups    []NormalizedClientGroup      `json:"clientGroups"`
	TotalClients    int                          `json:"totalClients"`
}

// NormalizedTCPConfig holds the effective transport settings after validation.
// Optional input settings have already been replaced by concrete defaults.
type NormalizedTCPConfig struct {
	ReceiveBufferBytes int                 `json:"receiveBufferBytes"`
	RTO                NormalizedRTOConfig `json:"rto"`
	ClientIdleTimeout  time.Duration       `json:"clientIdleTimeoutNanoseconds"`
}

// NormalizedRTOConfig holds the checked timer policy used by each TCP sender.
// For example, backoff can turn a 1-second retry delay into 2, then 4 seconds.
type NormalizedRTOConfig struct {
	Mode               string        `json:"mode"`
	ExponentialBackoff bool          `json:"exponentialBackoff"`
	InitialTimeout     time.Duration `json:"initialTimeoutNanoseconds"`
	MinimumTimeout     time.Duration `json:"minimumTimeoutNanoseconds"`
	MaximumTimeout     time.Duration `json:"maximumTimeoutNanoseconds"`
}

// NormalizedLoadBalancerConfig includes concrete idle limits for tracked connections.
// Zero keeps the corresponding connection state until another close condition occurs.
type NormalizedLoadBalancerConfig struct {
	ID                      string        `json:"id"`
	Algorithm               string        `json:"algorithm"`
	Ingress                 LinkConfig    `json:"ingress"`
	ForwardingBitsPerSecond int64         `json:"forwardingBitsPerSecond"`
	HalfOpenIdleTimeout     time.Duration `json:"halfOpenIdleTimeoutNanoseconds"`
	EstablishedIdleTimeout  time.Duration `json:"establishedIdleTimeoutNanoseconds"`
}

// DirectionalLinks holds the final settings for an endpoint's two link directions.
// ToLoadBalancer is client/backend -> LB; FromLoadBalancer is LB -> client/backend.
type DirectionalLinks struct {
	ToLoadBalancer   LinkConfig `json:"toLoadBalancer"`
	FromLoadBalancer LinkConfig `json:"fromLoadBalancer"`
}

// NormalizedBackend is produced from BackendConfig after validation.
// Its settings already include defaults, the selected profile and overrides.
type NormalizedBackend struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Address string           `json:"address"`
	Port    uint16           `json:"port"`
	Network DirectionalLinks `json:"network"`
}

// NormalizedClientGroup holds shared settings and resolved client exceptions.
// Client(index) retrieves one client's settings without expanding the whole group.
type NormalizedClientGroup struct {
	ID        string                     `json:"id"`
	Count     int                        `json:"count"`
	StartAt   time.Duration              `json:"startAtNanoseconds"`
	ClientGap time.Duration              `json:"clientGapNanoseconds"`
	Network   DirectionalLinks           `json:"network"`
	Stream    StreamConfig               `json:"stream"`
	Overrides []NormalizedClientOverride `json:"overrides"`
}

// NormalizedClientOverride holds the final settings of one client with overrides.
// Client(index) uses these settings when the requested index matches.
type NormalizedClientOverride struct {
	Index   int              `json:"index"`
	Network DirectionalLinks `json:"network"`
	Stream  StreamConfig     `json:"stream"`
}

// ClientConfig contains one client's resolved link and stream settings.
// Client(index) returns it with a stable ID derived from the group and index.
type ClientConfig struct {
	ID      string           `json:"id"`
	Network DirectionalLinks `json:"network"`
	Stream  StreamConfig     `json:"stream"`
}
