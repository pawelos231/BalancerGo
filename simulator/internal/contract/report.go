package contract

import (
	"encoding/json"
	"time"
)

// LinkConfig preserves the CLI's PascalCase fields and nanosecond duration.
type LinkConfig struct {
	BandwidthBitsPerSecond int64
	PacketTravelTime       time.Duration
	QueueCapacity          int
	LossRate               float64
}

// LinkStats preserves the CLI's complete physical and queue counters.
type LinkStats struct {
	OfferedPackets      uint64
	OfferedBytes        uint64
	AcceptedPackets     uint64
	AcceptedBytes       uint64
	QueueDroppedPackets uint64
	QueueDroppedBytes   uint64
	TransmittedBytes    uint64
	LostPackets         uint64
	LostBytes           uint64
	DeliveredPackets    uint64
	DeliveredBytes      uint64
	QueuedPackets       int
	Transmitting        bool
	TravellingPackets   int
}

type LinkResult struct {
	Name   string
	Config LinkConfig
	LinkStats
}

// IngressQueueResult separates client and backend traffic at the shared receiver.
type IngressQueueResult struct {
	Side     string
	Capacity int
	LinkStats
}

type ForwardingStats struct {
	ForwardingBitsPerSecond int64
	ForwardedPackets        uint64
	ForwardedBytes          uint64
	QueuedPackets           int
	QueueDroppedPackets     uint64
	QueueDroppedBytes       uint64
	BudgetWait              time.Duration
	BudgetBlocked           bool
}

type LoadBalancerState struct {
	Mode      LBMode                   `json:"mode"`
	Algorithm string                   `json:"algorithm"`
	Servers   []ServerStateWithClients `json:"servers"`
}

type ServerStateWithClients struct {
	Server  ServerState   `json:"server"`
	Clients []ClientState `json:"clients"`
}

type ServerState struct {
	ID                     string
	Name                   string
	Address                string
	Port                   uint16
	Active                 bool
	ActiveConnections      uint32
	HalfOpenConnections    uint32
	LatencyP95Milli        uint32
	NumberOfHandledPackets uint32
	ReceivedPayloadBytes   uint64
	CpuUsage               float32
}

type ClientState struct {
	Key                      ClientKey
	RetrySyn                 uint8
	RetryData                uint8
	AcknowledgedPayloadBytes uint64
	Transfer                 TransferState
}

// SimulationResult preserves the legacy headless report, including PascalCase
// fields outside its preexisting nested HTTP summaries and balancer state.
type SimulationResult struct {
	VirtualTime string
	WallTime    string
	Seed        int64

	ScheduledClients         int
	CreatedClients           int
	CompletedStreams         int
	ClosedClients            int
	IncompleteStreams        int
	CompletedPayloadBytes    uint64
	ReceivedPayloadBytes     uint64
	AcknowledgedPayloadBytes uint64
	Transfers                TransferSummary

	EventsExecuted uint64
	PendingEvents  int
	Ingress        LinkResult
	IngressQueues  []IngressQueueResult
	Forwarding     ForwardingStats
	Links          []LinkResult
	State          LoadBalancerState
}

// ExperimentReport packages one CLI run with its input and build information.
// Failed or cancelled execution still includes the work completed so far.
type ExperimentReport struct {
	Version  int                `json:"version"`
	Status   string             `json:"status"`
	Error    string             `json:"error,omitempty"`
	Manifest ExperimentManifest `json:"manifest"`
	Result   SimulationResult   `json:"result"`
}

// ExperimentManifest keeps the original importable document and identifies it.
// Execution records process choices that differ from the document, such as pacing.
type ExperimentManifest struct {
	Model          string              `json:"model"`
	Scenario       json.RawMessage     `json:"scenario"`
	ScenarioSHA256 string              `json:"scenarioSha256"`
	Execution      ExperimentExecution `json:"execution"`
	Build          ExperimentBuild     `json:"build"`
}

// ExperimentExecution describes how the program ran, outside virtual traffic.
type ExperimentExecution struct {
	Headless     bool    `json:"headless"`
	Speed        float64 `json:"speed"`
	TraceEnabled bool    `json:"traceEnabled"`
}

// ExperimentBuild identifies the executable's Go build and target platform.
// Missing VCS fields mean the build did not supply that information.
type ExperimentBuild struct {
	GoVersion     string `json:"goVersion"`
	ModuleVersion string `json:"moduleVersion"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	VCSRevision   string `json:"vcsRevision,omitempty"`
	VCSModified   *bool  `json:"vcsModified,omitempty"`
}
