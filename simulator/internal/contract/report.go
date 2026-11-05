package contract

import (
	"encoding/json"
	"time"
)

// LinkConfig preserves the CLI's PascalCase fields and nanosecond duration.
type LinkConfig struct {
	BandwidthBitsPerSecond      int64
	PacketTravelTime            time.Duration
	QueueCapacity               int
	LossRate                    float64
	QueuePolicy                 string  `json:",omitempty"`
	CongestionThresholdPercent  float64 `json:",omitempty"`
	CongestionSignalProbability float64 `json:",omitempty"`
}

// LinkStats preserves the CLI's complete physical and queue counters.
type LinkStats struct {
	OfferedPackets           uint64
	OfferedBytes             uint64
	AcceptedPackets          uint64
	AcceptedBytes            uint64
	QueueDroppedPackets      uint64
	QueueDroppedBytes        uint64
	ECNMarkedPackets         uint64 `json:",omitempty"`
	ECNMarkedBytes           uint64 `json:",omitempty"`
	CongestionDroppedPackets uint64 `json:",omitempty"`
	CongestionDroppedBytes   uint64 `json:",omitempty"`
	TransmittedBytes         uint64
	LostPackets              uint64
	LostBytes                uint64
	DeliveredPackets         uint64
	DeliveredBytes           uint64
	QueuedPackets            int
	Transmitting             bool
	TravellingPackets        int
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
	ForwardingBitsPerSecond  int64
	ForwardedPackets         uint64
	ForwardedBytes           uint64
	QueuedPackets            int
	QueueDroppedPackets      uint64
	QueueDroppedBytes        uint64
	ECNMarkedPackets         uint64 `json:",omitempty"`
	ECNMarkedBytes           uint64 `json:",omitempty"`
	CongestionDroppedPackets uint64 `json:",omitempty"`
	CongestionDroppedBytes   uint64 `json:",omitempty"`
	BudgetWait               time.Duration
	BudgetBlocked            bool
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
	ECN                    *ECNState `json:",omitempty"`
	ACK                    *ACKState `json:",omitempty"`
}

type ClientState struct {
	Key                      ClientKey
	RetrySyn                 uint8
	RetryData                uint8
	AcknowledgedPayloadBytes uint64
	Transfer                 TransferState
	Connection               CloseState
	ECN                      *ECNState `json:",omitempty"`
}

// ECNState separates queue feedback and sender reactions from packet-loss counters.
type ECNState struct {
	Enabled               bool
	Negotiated            bool
	ReceivedCEPacketCount uint64
	SentECECount          uint64
	ReceivedECECount      uint64
	SentCWRCount          uint64
	ECNResponseCount      uint64
}

// ACKState counts standalone ACKs, including the subset sent by the delayed timer.
type ACKState struct {
	Enabled                bool
	SentACKCount           uint64
	TimerTriggeredACKCount uint64
}

// DelayedACKConfig records receiver ACK grouping and its virtual waiting limit.
type DelayedACKConfig struct {
	Enabled        bool          `json:"enabled"`
	SegmentsPerACK int           `json:"segmentsPerACK"`
	MaxDelay       time.Duration `json:"maxDelayNanoseconds"`
}

type ClientECN struct {
	ClientID string
	State    ECNState
}

// CloseConfig records shutdown limits in virtual nanoseconds.
type CloseConfig struct {
	MSL            time.Duration `json:"mslNanoseconds"`
	FinWaitTimeout time.Duration `json:"finWaitTimeoutNanoseconds"`
	MaxFINRetries  int           `json:"maxFINRetries"`
}

// ClientConnection retains transport completion independently of payload success.
type ClientConnection struct {
	ClientID string     `json:"clientId"`
	State    CloseState `json:"state"`
}

// RTOState reports a connection's measurements and its current retry delay.
// Samples=0 distinguishes an unmeasured RTT from a measured zero-second RTT.
type RTOState struct {
	LatestRTTSeconds    float64
	SmoothedRTTSeconds  float64
	RTTVariationSeconds float64
	BaseRTOSeconds      float64
	CurrentRTOSeconds   float64
	Samples             uint64
}

// ClientTiming retains estimates after a client leaves the LB's connection table.
// ClientID is the stable scenario ID, such as "client/fast/0".
type ClientTiming struct {
	ClientID string
	RTO      RTOState
}

// CongestionState reports the sender's window separately from link queue sizes.
// In none mode the window/threshold are zero; BytesInFlight remains meaningful.
type CongestionState struct {
	Mode                    string
	CongestionWindowBytes   uint64
	SlowStartThresholdBytes uint64
	BytesInFlight           uint64
	Phase                   string
	DuplicateACKs           uint64 // Current run of duplicate ACKs; resets on progress or timeout.
	FastRetransmissions     uint64 // Cumulative retries triggered before the retransmission timeout.
}

// ClientCongestion retains the last sender state after completion or cleanup.
type ClientCongestion struct {
	ClientID string
	State    CongestionState
}

// CongestionConfig records the selected mode and configured startup settings.
type CongestionConfig struct {
	Mode                           string
	FastRetransmit                 bool
	CubicFastConvergence           bool
	InitialWindowSegments          int
	MaximumWindowSegments          int `json:",omitempty"`
	InitialSlowStartThresholdBytes uint64
}

// RTOConfig records the effective timer policy, including resolved defaults.
type RTOConfig struct {
	Mode                  string
	ExponentialBackoff    bool
	InitialTimeoutSeconds float64
	MinimumTimeoutSeconds float64
	MaximumTimeoutSeconds float64
}

// TCPConfig records the effective transport settings, including input defaults.
type TCPConfig struct {
	MSSBytes                             int `json:"mssBytes"`
	ReceiveBufferBytes                   int
	SACKEnabled                          bool
	ECNEnabled                           bool             `json:"ECNenabled,omitempty"`
	DelayedACK                           DelayedACKConfig `json:"delayedAck"`
	CongestionControl                    CongestionConfig
	RTO                                  RTOConfig
	ClientIdleTimeoutSeconds             float64
	BackendHalfOpenIdleTimeoutSeconds    float64
	BackendEstablishedIdleTimeoutSeconds float64
}

// LBIdleTimeoutConfig records when the balancer forgets inactive connections.
// A zero timeout disables cleanup for that connection state.
type LBIdleTimeoutConfig struct {
	HalfOpenIdleTimeoutSeconds    float64
	EstablishedIdleTimeoutSeconds float64
	ClosingIdleTimeoutSeconds     float64
	ResetIdleTimeoutSeconds       float64
}

// SimulationResult preserves the legacy headless report, including PascalCase
// fields outside its preexisting nested HTTP summaries and balancer state.
type SimulationResult struct {
	VirtualTime      string
	WallTime         string
	Seed             int64
	TCP              TCPConfig
	LBIdleTimeouts   LBIdleTimeoutConfig
	ClientTimings    []ClientTiming
	ClientCongestion []ClientCongestion
	ClientECN        []ClientECN        `json:",omitempty"`
	EffectiveClose   CloseConfig        `json:"effectiveClose"`
	ClientConnection []ClientConnection `json:"clientConnection"`

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

// ExperimentSeriesReport indexes independent runs of one base scenario.
// Manifest keeps the base input; each run report stores its actual seed.
type ExperimentSeriesReport struct {
	Version  int                   `json:"version"`
	Status   string                `json:"status"`
	Error    string                `json:"error,omitempty"`
	Manifest ExperimentManifest    `json:"manifest"`
	Runs     []ExperimentSeriesRun `json:"runs"`
}

// ExperimentSeriesRun records progress without retaining a simulation's state.
// Seed is an exact decimal string; Report appears only after a successful save.
type ExperimentSeriesRun struct {
	ID     string `json:"id"`
	Seed   string `json:"seed"`
	Status string `json:"status"`
	Report string `json:"report,omitempty"`
	Error  string `json:"error,omitempty"`
}
