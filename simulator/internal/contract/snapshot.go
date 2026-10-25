package contract

// Protocol is the transport name shown in public client keys.
type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP"
)

type ClientKey struct {
	SrcIP    string
	SrcPort  uint16
	DstIP    string
	DstPort  uint16
	Protocol Protocol
}

type LBMode string

const (
	LBModeNAT  LBMode = "NAT"
	LBModeSNAT LBMode = "SNAT"
	LBModeDSR  LBMode = "DSR"
)

// PayloadProgress counts unique payload bytes, excluding headers and repeats.
type PayloadProgress struct {
	ReceivedBytes     uint64 `json:"receivedBytes"`
	AcknowledgedBytes uint64 `json:"acknowledgedBytes"`
}

// SimulationPerformance measures real execution separately from virtual traffic.
type SimulationPerformance struct {
	WallTimeSeconds               float64 `json:"wallTimeSeconds"`
	EventsExecuted                uint64  `json:"eventsExecuted"`
	EventsPerWallSecond           float64 `json:"eventsPerWallSecond"`
	ForwardedPackets              uint64  `json:"forwardedPackets"`
	ForwardedPacketsPerWallSecond float64 `json:"forwardedPacketsPerWallSecond"`
}

// DurationSummary describes completed measurements in virtual seconds.
// Samples distinguishes an unavailable measurement from a measured zero duration.
type DurationSummary struct {
	Samples     uint64  `json:"samples"`
	MeanSeconds float64 `json:"meanSeconds"`
	MinSeconds  float64 `json:"minSeconds"`
	MaxSeconds  float64 `json:"maxSeconds"`
}

// TransferSummary retains outcomes after endpoint and routing cleanup.
type TransferSummary struct {
	Started    int `json:"started"`
	NotStarted int `json:"notStarted"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	InProgress int `json:"inProgress"`

	Failures map[TransferFailureReason]uint64 `json:"failures"`

	SynRetransmissions  uint64 `json:"synRetransmissions"`
	DataRetransmissions uint64 `json:"dataRetransmissions"`

	Handshake  DurationSummary `json:"handshake"`
	Payload    DurationSummary `json:"payload"`
	Completion DurationSummary `json:"completion"`
}

// SimulationMetadata describes the browser-controlled experiment lifecycle.
type SimulationMetadata struct {
	Status             string                 `json:"status"`
	RunID              string                 `json:"runId,omitempty"`
	Algorithm          string                 `json:"algorithm"`
	VirtualTimeSeconds float64                `json:"virtualTimeSeconds"`
	Error              string                 `json:"error,omitempty"`
	Performance        *SimulationPerformance `json:"performance,omitempty"`
	Payload            *PayloadProgress       `json:"payload,omitempty"`
	Transfers          *TransferSummary       `json:"transfers,omitempty"`
}

type SimulationSnapshot struct {
	Type       string             `json:"type"`
	Version    int                `json:"version"`
	Mode       LBMode             `json:"mode"`
	Algorithm  string             `json:"algorithm"`
	Servers    []TelemetryServer  `json:"servers"`
	Simulation SimulationMetadata `json:"simulation"`
	Topology   TelemetryTopology  `json:"topology"`
}

// Topology is a detached view of the nodes and directed links actually wired
// into a simulation. Empty slices represent the absence of a current model.
type Topology struct {
	Nodes   []TopologyNode   `json:"nodes"`
	Links   []TopologyLink   `json:"links"`
	Ingress *TopologyIngress `json:"ingress,omitempty"`
}

// TopologyIngress shows one shared receiver and the queues feeding it.
// Its bitrate applies once across both sides of the load balancer.
type TopologyIngress struct {
	Config TopologyLinkConfig   `json:"config"`
	Stats  TopologyLinkStats    `json:"stats"`
	Queues []TopologyInputQueue `json:"queues"`
}

// TopologyInputQueue holds traffic arriving from one side of the LB.
// Capacity counts waiting packets; both queues share the ingress transmitter.
type TopologyInputQueue struct {
	ID       string            `json:"id"`
	Side     string            `json:"side"`
	Label    string            `json:"label"`
	Capacity int               `json:"capacity"`
	Stats    TopologyLinkStats `json:"stats"`
}

const (
	TopologyNodeClient            = "client"
	TopologyNodeClientGroup       = "clientGroup"
	TopologyNodeLoadBalancer      = "loadBalancer"
	TopologyNodeBackend           = "backend"
	TopologyNodeNetworkAggregator = "networkAggregator"
)

// Kind remains an open string so additional node types can use the same graph
// contract. Metrics describe each node without requiring a fixed stats schema.
type TopologyNode struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Label       string           `json:"label"`
	ResourceID  string           `json:"resourceId,omitempty"`
	GroupID     string           `json:"groupId,omitempty"`
	Description string           `json:"description,omitempty"`
	Address     string           `json:"address,omitempty"`
	Active      *bool            `json:"active,omitempty"`
	Metrics     []TopologyMetric `json:"metrics"`
	Traffic     *TopologyTraffic `json:"traffic,omitempty"`
}

// TopologyTraffic measures wire bytes over the last completed virtual-time window.
// TX counts completed serialization; RX counts packets delivered to this node.
type TopologyTraffic struct {
	TransmitBytesPerSecond float64 `json:"transmitBytesPerSecond"`
	ReceiveBytesPerSecond  float64 `json:"receiveBytesPerSecond"`
	SampleWindowSeconds    float64 `json:"sampleWindowSeconds"`
}

type TopologyMetric struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

type TopologyLink struct {
	ID     string             `json:"id"`
	Label  string             `json:"label"`
	Source string             `json:"source"`
	Target string             `json:"target"`
	Config TopologyLinkConfig `json:"config"`
	Stats  TopologyLinkStats  `json:"stats"`
}

type TopologyLinkConfig struct {
	BandwidthBitsPerSecond int64   `json:"bandwidthBitsPerSecond"`
	PacketTravelTimeMs     float64 `json:"packetTravelTimeMs"`
	QueueCapacity          int     `json:"queueCapacity"`
	LossRate               float64 `json:"lossRate"`
}

type TopologyLinkStats struct {
	QueuedPackets       int    `json:"queuedPackets"`
	Transmitting        bool   `json:"transmitting"`
	TravellingPackets   int    `json:"travellingPackets"`
	OfferedPackets      uint64 `json:"offeredPackets"`
	OfferedBytes        uint64 `json:"offeredBytes"`
	AcceptedPackets     uint64 `json:"acceptedPackets"`
	AcceptedBytes       uint64 `json:"acceptedBytes"`
	DeliveredPackets    uint64 `json:"deliveredPackets"`
	QueueDroppedPackets uint64 `json:"queueDroppedPackets"`
	QueueDroppedBytes   uint64 `json:"queueDroppedBytes"`
	LostPackets         uint64 `json:"lostPackets"`
	LostBytes           uint64 `json:"lostBytes"`
	DeliveredBytes      uint64 `json:"deliveredBytes"`
}

// TopologyDocument defines the resources used by one run and topology revision.
// Its contents stay unchanged until a new revision is published.
type TopologyDocument struct {
	RunID    string                     `json:"runId"`
	Revision string                     `json:"revision"`
	Nodes    []TopologyNodeDefinition   `json:"nodes"`
	Links    []TopologyLinkDefinition   `json:"links"`
	Ingress  *TopologyIngressDefinition `json:"ingress,omitempty"`
	Servers  []ServerDefinition         `json:"servers"`
}

// TopologyNodeDefinition keeps identity, display metadata and metric definitions.
// ClientKey lets the browser restore routed-client details from compact IDs.
type TopologyNodeDefinition struct {
	ID          string                     `json:"id"`
	Kind        string                     `json:"kind"`
	Label       string                     `json:"label"`
	ResourceID  string                     `json:"resourceId,omitempty"`
	GroupID     string                     `json:"groupId,omitempty"`
	Description string                     `json:"description,omitempty"`
	Address     string                     `json:"address,omitempty"`
	Metrics     []TopologyMetricDefinition `json:"metrics"`
	ClientKey   *ClientKey                 `json:"clientKey,omitempty"`
}

// TopologyMetricDefinition supplies labels once; static metrics also carry a value.
type TopologyMetricDefinition struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Unit  string   `json:"unit,omitempty"`
	Value *float64 `json:"value,omitempty"`
}

// TopologyLinkDefinition describes one directed physical path without live counters.
type TopologyLinkDefinition struct {
	ID     string             `json:"id"`
	Label  string             `json:"label"`
	Source string             `json:"source"`
	Target string             `json:"target"`
	Config TopologyLinkConfig `json:"config"`
}

// TopologyIngressDefinition keeps the shared receiver and its separate FIFO limits.
type TopologyIngressDefinition struct {
	Config TopologyLinkConfig             `json:"config"`
	Queues []TopologyInputQueueDefinition `json:"queues"`
}

// TopologyInputQueueDefinition identifies a FIFO without repeating its live state.
type TopologyInputQueueDefinition struct {
	ID       string `json:"id"`
	Side     string `json:"side"`
	Label    string `json:"label"`
	Capacity int    `json:"capacity"`
}

// ServerDefinition preserves dashboard identity separately from changing counters.
type ServerDefinition struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}

// TelemetryTopology is a full metrics snapshot for exactly one definition revision.
// Missing intermediate frames do not require replaying earlier updates.
type TelemetryTopology struct {
	Revision string            `json:"revision"`
	Nodes    []TelemetryNode   `json:"nodes"`
	Links    []TelemetryLink   `json:"links"`
	Ingress  *TelemetryIngress `json:"ingress,omitempty"`
}

// TelemetryNode contains changing values; metric labels and constants stay in HTTP.
type TelemetryNode struct {
	ID      string             `json:"id"`
	Active  *bool              `json:"active,omitempty"`
	Metrics map[string]float64 `json:"metrics"`
	Traffic *TopologyTraffic   `json:"traffic,omitempty"`
}

// TelemetryLink sends one physical path's counters under its stable ID.
type TelemetryLink struct {
	ID    string            `json:"id"`
	Stats TopologyLinkStats `json:"stats"`
}

// TelemetryIngress reports shared physical progress and both input queues.
type TelemetryIngress struct {
	Stats  TopologyLinkStats     `json:"stats"`
	Queues []TelemetryInputQueue `json:"queues"`
}

// TelemetryInputQueue carries live counters for one defined input FIFO.
type TelemetryInputQueue struct {
	ID    string            `json:"id"`
	Stats TopologyLinkStats `json:"stats"`
}

// TelemetryServer sends live dashboard state without repeating server addresses.
type TelemetryServer struct {
	ID                     string            `json:"id"`
	Active                 bool              `json:"active"`
	ActiveConnections      uint32            `json:"activeConnections"`
	HalfOpenConnections    uint32            `json:"halfOpenConnections"`
	LatencyP95Milli        uint32            `json:"latencyP95Milli"`
	NumberOfHandledPackets uint32            `json:"numberOfHandledPackets"`
	ReceivedPayloadBytes   uint64            `json:"receivedPayloadBytes"`
	CpuUsage               float32           `json:"cpuUsage"`
	Clients                []TelemetryClient `json:"clients"`
}

// TelemetryClient refers to the full client key in its node definition.
type TelemetryClient struct {
	ID                       string        `json:"id"`
	RetrySyn                 uint8         `json:"retrySyn"`
	RetryData                uint8         `json:"retryData"`
	AcknowledgedPayloadBytes uint64        `json:"acknowledgedPayloadBytes"`
	Transfer                 TransferState `json:"transfer"`
}

// TransferStatus describes delivery of a client's payload, independently of
// whether its TCP connection has finished closing.
type TransferStatus string

const (
	TransferInProgress TransferStatus = "inProgress"
	TransferSucceeded  TransferStatus = "succeeded"
	TransferFailed     TransferStatus = "failed"
)

// TransferFailureReason records the existing action that ended an unfinished
// transfer. It does not infer the remote cause of a timeout.
type TransferFailureReason string

const (
	TransferSynRetryLimit   TransferFailureReason = "synRetryLimit"
	TransferDataRetryLimit  TransferFailureReason = "dataRetryLimit"
	TransferReadTimeout     TransferFailureReason = "readTimeout"
	TransferRemoteClosed    TransferFailureReason = "remoteClosed"
	TransferClosedEarly     TransferFailureReason = "closedEarly"
	TransferInterrupted     TransferFailureReason = "interrupted"
	TransferBalancerTimeout TransferFailureReason = "balancerTimeout"
	TransferBackendRemoved  TransferFailureReason = "backendRemoved"
)

// TransferState is a detached measurement of one attempt in virtual time.
// Handshake duration requires HandshakeCompleted; payload duration requires success.
type TransferState struct {
	Status                   TransferStatus        `json:"status"`
	FailureReason            TransferFailureReason `json:"failureReason,omitempty"`
	ElapsedSeconds           float64               `json:"elapsedSeconds"`
	HandshakeCompleted       bool                  `json:"handshakeCompleted"`
	HandshakeDurationSeconds float64               `json:"handshakeDurationSeconds"`
	PayloadDurationSeconds   float64               `json:"payloadDurationSeconds"`
	SynRetransmissions       uint64                `json:"synRetransmissions"`
	DataRetransmissions      uint64                `json:"dataRetransmissions"`
}
