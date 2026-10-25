package model

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
