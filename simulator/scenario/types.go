// Package scenario defines simulation configuration and validates its settings.
// It combines defaults, profiles and individual overrides into resolved values.
package scenario

import "time"

const (
	MaxClients  = (1 << 24) - 1
	MaxBackends = 10000
)

// TCPConfig separates transport timers and receive buffering from network links.
// Omitted settings inherit defaults; a zero idle timeout disables idle cleanup.
type TCPConfig struct {
	MSSBytes                      *int             `json:"mssBytes,omitempty"` // Maximum payload per data segment; no handshake negotiation.
	ReceiveBufferBytes            *int             `json:"receiveBufferBytes,omitempty"`
	SACKEnabled                   *bool            `json:"sackEnabled,omitempty"`
	ECNEnabled                    *bool            `json:"ECNenabled,omitempty"`
	DelayedACK                    DelayedACKConfig `json:"delayedAck"`
	RTO                           RTOConfig        `json:"rto"`
	Close                         CloseConfig      `json:"close"`
	CongestionControl             CongestionConfig `json:"congestionControl"`
	ClientIdleTimeout             *time.Duration   `json:"clientIdleTimeoutNanoseconds,omitempty"`
	BackendHalfOpenIdleTimeout    *time.Duration   `json:"backendHalfOpenIdleTimeoutNanoseconds,omitempty"`
	BackendEstablishedIdleTimeout *time.Duration   `json:"backendEstablishedIdleTimeoutNanoseconds,omitempty"`
}

// DelayedACKConfig groups ordinary data ACKs while keeping a bounded virtual wait.
// Omitted fields use immediate ACKs, a two-segment threshold and a 40 ms timer.
type DelayedACKConfig struct {
	Enabled        *bool          `json:"enabled,omitempty"`
	SegmentsPerACK *int           `json:"segmentsPerACK,omitempty"`
	MaxDelay       *time.Duration `json:"maxDelayNanoseconds,omitempty"`
}

// CloseConfig bounds orderly shutdown while retaining duplicate-FIN protection.
// Pointers retain omitted defaults and an explicitly disabled FIN retry budget.
type CloseConfig struct {
	MSL            *time.Duration `json:"mslNanoseconds,omitempty"`
	FinWaitTimeout *time.Duration `json:"finWaitTimeoutNanoseconds,omitempty"`
	MaxFINRetries  *int           `json:"maxFINRetries,omitempty"`
}

// CongestionConfig limits unacknowledged data as the sender learns network capacity.
// Omitted fields use defaults; mode none keeps only the receiver's window limit.
type CongestionConfig struct {
	Mode                           *string `json:"mode,omitempty"`
	FastRetransmit                 *bool   `json:"fastRetransmit,omitempty"`
	CubicFastConvergence           *bool   `json:"cubicFastConvergence,omitempty"`
	InitialWindowSegments          *int    `json:"initialWindowSegments,omitempty"`
	MaximumWindowSegments          *int    `json:"maximumWindowSegments,omitempty"`
	InitialSlowStartThresholdBytes *uint64 `json:"initialSlowStartThresholdBytes,omitempty"`
}

// RTOConfig controls how long the sender waits before retrying a missing ACK.
// Pointers preserve omitted defaults and an explicitly disabled backoff.
type RTOConfig struct {
	Mode               *string        `json:"mode,omitempty"`
	ExponentialBackoff *bool          `json:"exponentialBackoff,omitempty"`
	InitialTimeout     *time.Duration `json:"initialTimeoutNanoseconds,omitempty"`
	MinimumTimeout     *time.Duration `json:"minimumTimeoutNanoseconds,omitempty"`
	MaximumTimeout     *time.Duration `json:"maximumTimeoutNanoseconds,omitempty"`
}

// ExecutionConfig controls virtual run length, real-time UI refreshes and randomness.
// It tells the runner when to stop and how fast to advance virtual time.
type ExecutionConfig struct {
	Duration         time.Duration `json:"durationNanoseconds"`
	SnapshotInterval time.Duration `json:"snapshotIntervalNanoseconds"` // Real time between UI snapshots.

	// Seed makes random routing and loss reproducible. Future models may use it
	// for traffic generation, network/CPU timing, chaos and telemetry.
	Seed      int64   `json:"seed"`
	Speed     float64 `json:"speed"`
	MaxEvents uint64  `json:"maxEvents"`
}

// NetworkConfig holds common link settings and reusable named profiles.
// Endpoints inherit these settings before applying their own overrides.
type NetworkConfig struct {
	Defaults LinkConfig    `json:"defaults"`
	Profiles []LinkProfile `json:"profiles,omitempty"`
}

// LinkConfig describes one-way link speed, travel time, buffering and loss.
// For links leaving the LB, QueueCapacity sizes the FIFO owned by Egress.
type LinkConfig struct {
	BandwidthBitsPerSecond      int64         `json:"bandwidthBitsPerSecond"`
	PacketTravelTime            time.Duration `json:"packetTravelTimeNanoseconds"`
	QueueCapacity               int           `json:"queueCapacity"`
	LossRate                    float64       `json:"lossRate"`
	QueuePolicy                 string        `json:"queuePolicy,omitempty"`
	CongestionThresholdPercent  float64       `json:"congestionThresholdPercent"`
	CongestionSignalProbability float64       `json:"congestionSignalProbability"`
}

// LinkPatch changes selected link settings inherited from defaults or a profile.
// Nil keeps the inherited value; a pointer to zero explicitly sets zero.
type LinkPatch struct {
	BandwidthBitsPerSecond      *int64         `json:"bandwidthBitsPerSecond,omitempty"`
	PacketTravelTime            *time.Duration `json:"packetTravelTimeNanoseconds,omitempty"`
	QueueCapacity               *int           `json:"queueCapacity,omitempty"`
	LossRate                    *float64       `json:"lossRate,omitempty"`
	QueuePolicy                 *string        `json:"queuePolicy,omitempty"`
	CongestionThresholdPercent  *float64       `json:"congestionThresholdPercent,omitempty"`
	CongestionSignalProbability *float64       `json:"congestionSignalProbability,omitempty"`
}

// LinkProfile names reusable link overrides, such as "slow-client".
// Clients and backends select it by ID instead of repeating its settings.
type LinkProfile struct {
	ID     string    `json:"id"`
	Config LinkPatch `json:"config"`
}

// EndpointNetworkConfig selects a client's or backend's link profile.
// Directional overrides then adjust traffic to and from the load balancer.
type EndpointNetworkConfig struct {
	Profile          string    `json:"profile,omitempty"`
	ToLoadBalancer   LinkPatch `json:"toLoadBalancer"`
	FromLoadBalancer LinkPatch `json:"fromLoadBalancer"`
}

// HostConfig limits packet processing separately from links and TCP buffering.
// Zero processing time disables the CPU model; omitted values use safe defaults.
type HostConfig struct {
	Cores                *int           `json:"cores,omitempty"`
	PacketProcessingTime *time.Duration `json:"packetProcessingTimeNanoseconds,omitempty"`
	QueueCapacity        *int           `json:"queueCapacity,omitempty"`
}

// StreamConfig describes a client's payload size and pause between data segments.
// Bytes overrides Packets; -1 means Packets multiplied by a fixed 1,024-byte unit.
type StreamConfig struct {
	Packets   int           `json:"packets"`
	Bytes     int           `json:"bytes"`
	PacketGap time.Duration `json:"packetGapNanoseconds"`
}

// StreamPatch changes a group's or client's inherited stream settings.
// Nil keeps the inherited value; a pointer can explicitly set a field to zero.
type StreamPatch struct {
	Packets   *int           `json:"packets,omitempty"`
	Bytes     *int           `json:"bytes,omitempty"`
	PacketGap *time.Duration `json:"packetGapNanoseconds,omitempty"`
}

// ClientGroup describes many clients sharing arrival, stream and link settings.
// Overrides customize individual clients without listing every client separately.
type ClientGroup struct {
	ID        string                `json:"id"`
	Count     int                   `json:"count"`
	StartAt   time.Duration         `json:"startAtNanoseconds"`
	ClientGap time.Duration         `json:"clientGapNanoseconds"`
	Network   EndpointNetworkConfig `json:"network"`
	Stream    StreamPatch           `json:"stream"`
	Overrides []ClientOverride      `json:"overrides,omitempty"`
}

// ClientOverride changes one client's settings within a group, indexed from zero.
// Its link and stream overrides are applied after the group's settings.
type ClientOverride struct {
	Index   int                   `json:"index"`
	Network EndpointNetworkConfig `json:"network"`
	Stream  StreamPatch           `json:"stream"`
}
