package scenario

import "time"

// NormalizedTCPConfig holds the effective transport settings after validation.
// Optional input settings have already been replaced by concrete defaults.
type NormalizedTCPConfig struct {
	MSSBytes                      int                        `json:"mssBytes"`
	ReceiveBufferBytes            int                        `json:"receiveBufferBytes"`
	SACKEnabled                   bool                       `json:"sackEnabled"`
	ECNEnabled                    bool                       `json:"ECNenabled"`
	DelayedACK                    NormalizedDelayedACKConfig `json:"delayedAck"`
	RTO                           NormalizedRTOConfig        `json:"rto"`
	Close                         NormalizedCloseConfig      `json:"close"`
	CongestionControl             NormalizedCongestionConfig `json:"congestionControl"`
	ClientIdleTimeout             time.Duration              `json:"clientIdleTimeoutNanoseconds"`
	BackendHalfOpenIdleTimeout    time.Duration              `json:"backendHalfOpenIdleTimeoutNanoseconds"`
	BackendEstablishedIdleTimeout time.Duration              `json:"backendEstablishedIdleTimeoutNanoseconds"`
}

// NormalizedDelayedACKConfig contains the checked ACK grouping and virtual timer.
type NormalizedDelayedACKConfig struct {
	Enabled        bool          `json:"enabled"`
	SegmentsPerACK int           `json:"segmentsPerACK"`
	MaxDelay       time.Duration `json:"maxDelayNanoseconds"`
}

// NormalizedCloseConfig contains the effective shutdown limits.
type NormalizedCloseConfig struct {
	MSL            time.Duration `json:"mslNanoseconds"`
	FinWaitTimeout time.Duration `json:"finWaitTimeoutNanoseconds"`
	MaxFINRetries  int           `json:"maxFINRetries"`
}

// NormalizedCongestionConfig holds a checked starting window and growth policy.
// Segment counts become byte windows using TCP.MSSBytes.
type NormalizedCongestionConfig struct {
	Mode                           string `json:"mode"`
	FastRetransmit                 bool   `json:"fastRetransmit"`
	CubicFastConvergence           bool   `json:"cubicFastConvergence"`
	InitialWindowSegments          int    `json:"initialWindowSegments"`
	MaximumWindowSegments          int    `json:"maximumWindowSegments,omitempty"`
	InitialSlowStartThresholdBytes uint64 `json:"initialSlowStartThresholdBytes"`
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
	ID                      string               `json:"id"`
	Algorithm               string               `json:"algorithm"`
	Host                    NormalizedHostConfig `json:"host"`
	Ingress                 LinkConfig           `json:"ingress"`
	ForwardingBitsPerSecond int64                `json:"forwardingBitsPerSecond"`
	HalfOpenIdleTimeout     time.Duration        `json:"halfOpenIdleTimeoutNanoseconds"`
	EstablishedIdleTimeout  time.Duration        `json:"establishedIdleTimeoutNanoseconds"`
	ClosingIdleTimeout      time.Duration        `json:"closingIdleTimeoutNanoseconds"`
	ResetIdleTimeout        time.Duration        `json:"resetIdleTimeoutNanoseconds"`
}

// DirectionalLinks holds the final settings for an endpoint's two link directions.
// ToLoadBalancer is client/backend -> LB; FromLoadBalancer is LB -> client/backend.
type DirectionalLinks struct {
	ToLoadBalancer   LinkConfig `json:"toLoadBalancer"`
	FromLoadBalancer LinkConfig `json:"fromLoadBalancer"`
}

// NormalizedBackend contains checked endpoint, host and resolved link settings.
// Its settings already include defaults, the selected profile and overrides.
type NormalizedBackend struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Address string               `json:"address"`
	Port    uint16               `json:"port"`
	Network DirectionalLinks     `json:"network"`
	Host    NormalizedHostConfig `json:"host"`
}

// NormalizedHostConfig holds concrete CPU limits after omitted fields are resolved.
// QueueCapacity counts waiting packets; active packets occupy virtual cores.
type NormalizedHostConfig struct {
	Cores                int           `json:"cores"`
	PacketProcessingTime time.Duration `json:"packetProcessingTimeNanoseconds"`
	QueueCapacity        int           `json:"queueCapacity"`
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
