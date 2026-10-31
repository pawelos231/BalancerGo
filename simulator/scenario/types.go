// Package scenario defines simulation configuration and validates its settings.
// It combines defaults, profiles and individual overrides into resolved values.
package scenario

import "time"

const (
	Version     = 1
	MaxClients  = (1 << 24) - 1
	MaxBackends = 10000
)

// Scenario describes one experiment, from clients and backends to link settings.
// Normalize checks it and fills in inherited settings.
type Scenario struct {
	Version   int             `json:"version"`
	Execution ExecutionConfig `json:"execution"`
	TCP       TCPConfig       `json:"tcp"`
	Network   NetworkConfig   `json:"network"`
	Resources ResourcesConfig `json:"resources"`
	Workload  WorkloadConfig  `json:"workload"`
}

// TCPConfig separates transport timers and receive buffering from network links.
// Omitted settings inherit defaults; a zero idle timeout disables idle cleanup.
type TCPConfig struct {
	ReceiveBufferBytes *int             `json:"receiveBufferBytes,omitempty"`
	RTO                RTOConfig        `json:"rto"`
	CongestionControl  CongestionConfig `json:"congestionControl"`
	ClientIdleTimeout  *time.Duration   `json:"clientIdleTimeoutNanoseconds,omitempty"`
}

// CongestionConfig limits unacknowledged data as the sender learns network capacity.
// Omitted fields use defaults; mode none keeps only the receiver's window limit.
type CongestionConfig struct {
	Mode                           *string `json:"mode,omitempty"`
	InitialWindowSegments          *int    `json:"initialWindowSegments,omitempty"`
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

// ExecutionConfig controls virtual run length, pacing, sampling and randomness.
// It tells the runner when to stop and how fast to advance virtual time.
type ExecutionConfig struct {
	Duration         time.Duration `json:"durationNanoseconds"`
	SnapshotInterval time.Duration `json:"snapshotIntervalNanoseconds"`

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
	BandwidthBitsPerSecond int64         `json:"bandwidthBitsPerSecond"`
	PacketTravelTime       time.Duration `json:"packetTravelTimeNanoseconds"`
	QueueCapacity          int           `json:"queueCapacity"`
	LossRate               float64       `json:"lossRate"`
}

// LinkPatch changes selected link settings inherited from defaults or a profile.
// Nil keeps the inherited value; a pointer to zero explicitly sets zero.
type LinkPatch struct {
	BandwidthBitsPerSecond *int64         `json:"bandwidthBitsPerSecond,omitempty"`
	PacketTravelTime       *time.Duration `json:"packetTravelTimeNanoseconds,omitempty"`
	QueueCapacity          *int           `json:"queueCapacity,omitempty"`
	LossRate               *float64       `json:"lossRate,omitempty"`
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

// ResourcesConfig lists the balancer and backend servers in the experiment.
// Normalize checks their IDs and resolves each backend's links.
type ResourcesConfig struct {
	LoadBalancer LoadBalancerConfig `json:"loadBalancer"`
	Backends     []BackendConfig    `json:"backends"`
}

// LoadBalancerConfig separates the shared input from backend selection and forwarding.
// Ingress.QueueCapacity sizes each input queue; outputs use endpoint settings.
type LoadBalancerConfig struct {
	ID                      string         `json:"id"`
	Algorithm               string         `json:"algorithm"`
	Ingress                 LinkConfig     `json:"ingress"`
	ForwardingBitsPerSecond int64          `json:"forwardingBitsPerSecond"`
	HalfOpenIdleTimeout     *time.Duration `json:"halfOpenIdleTimeoutNanoseconds,omitempty"`
	EstablishedIdleTimeout  *time.Duration `json:"establishedIdleTimeoutNanoseconds,omitempty"`
}

// BackendConfig identifies one server and its network settings.
// Its address and port describe the destination for assigned connections.
type BackendConfig struct {
	ID      string                `json:"id"`
	Name    string                `json:"name,omitempty"`
	Address string                `json:"address"`
	Port    uint16                `json:"port"`
	Network EndpointNetworkConfig `json:"network"`
}

// WorkloadConfig describes how much data clients send and how they are grouped.
// Stream defaults apply unless a group or individual client overrides them.
type WorkloadConfig struct {
	Defaults     StreamConfig  `json:"defaults"`
	ClientGroups []ClientGroup `json:"clientGroups"`
}

// StreamConfig describes a client's payload size and pause between data segments.
// Bytes overrides Packets; -1 means Packets multiplied by the segment size.
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
