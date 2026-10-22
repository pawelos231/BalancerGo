package model

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
