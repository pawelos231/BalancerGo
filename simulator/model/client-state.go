package model

type ClientState struct {
	Connection               CloseState
	Key                      ClientKey
	RetrySyn                 uint8
	RetryData                uint8
	AcknowledgedPayloadBytes uint64
	Transfer                 TransferState
	RTO                      RTOState
	Congestion               CongestionState
	ECN                      ECNState
}
