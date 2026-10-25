package main

import "load-balancer/model"

// durationSummary describes completed measurements in virtual seconds.
// Samples distinguishes an unavailable measurement from a measured zero duration.
type durationSummary struct {
	Samples     uint64  `json:"samples"`
	MeanSeconds float64 `json:"meanSeconds"`
	MinSeconds  float64 `json:"minSeconds"`
	MaxSeconds  float64 `json:"maxSeconds"`
}

// add includes one duration without retaining or sorting every historical sample.
func (summary *durationSummary) add(seconds float64) {
	summary.Samples++
	summary.MeanSeconds += (seconds - summary.MeanSeconds) / float64(summary.Samples)
	if summary.Samples == 1 || seconds < summary.MinSeconds {
		summary.MinSeconds = seconds
	}
	if seconds > summary.MaxSeconds {
		summary.MaxSeconds = seconds
	}
}

// transferSummary counts successful, failed and ongoing transfers, even after cleanup.
// Clients scheduled for a future arrival are counted separately.
type transferSummary struct {
	Started    int `json:"started"`
	NotStarted int `json:"notStarted"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	InProgress int `json:"inProgress"`

	Failures map[model.TransferFailureReason]uint64 `json:"failures"`

	SynRetransmissions  uint64 `json:"synRetransmissions"`
	DataRetransmissions uint64 `json:"dataRetransmissions"`

	Handshake  durationSummary `json:"handshake"`
	Payload    durationSummary `json:"payload"`
	Completion durationSummary `json:"completion"`
}

// transferResults reads retained clients so closed and unrouted attempts still count.
// Merely reaching the run's end time does not fail unfinished transfers.
func (s *simulation) transferResults() transferSummary {
	summary := transferSummary{
		Started:    s.createdClients,
		NotStarted: s.scheduledClients - s.createdClients,
		Failures:   make(map[model.TransferFailureReason]uint64),
	}
	for _, runtime := range s.clients {
		transfer := runtime.Client.GetTransferState()
		summary.SynRetransmissions += transfer.SynRetransmissions
		summary.DataRetransmissions += transfer.DataRetransmissions
		if transfer.HandshakeCompleted {
			summary.Handshake.add(transfer.HandshakeDurationSeconds)
		}

		switch transfer.Status {
		case model.TransferSucceeded:
			summary.Succeeded++
			summary.Payload.add(transfer.PayloadDurationSeconds)
			summary.Completion.add(transfer.ElapsedSeconds)
		case model.TransferFailed:
			summary.Failed++
			summary.Failures[transfer.FailureReason]++
		default:
			summary.InProgress++
		}
	}
	return summary
}
