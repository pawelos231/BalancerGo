package model

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
	ID                       string `json:"id"`
	RetrySyn                 uint8  `json:"retrySyn"`
	RetryData                uint8  `json:"retryData"`
	AcknowledgedPayloadBytes uint64 `json:"acknowledgedPayloadBytes"`
}
