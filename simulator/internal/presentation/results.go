package presentation

import (
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/network"
)

func clientKey(key model.ClientKey) contract.ClientKey {
	return contract.ClientKey{
		SrcIP:    key.SrcIP,
		SrcPort:  key.SrcPort,
		DstIP:    key.DstIP,
		DstPort:  key.DstPort,
		Protocol: contract.Protocol(key.Protocol),
	}
}

func transferState(state model.TransferState) contract.TransferState {
	return contract.TransferState{
		Status:                   contract.TransferStatus(state.Status),
		FailureReason:            contract.TransferFailureReason(state.FailureReason),
		ElapsedSeconds:           state.Elapsed.Seconds(),
		HandshakeCompleted:       state.HandshakeCompleted,
		HandshakeDurationSeconds: state.HandshakeDuration.Seconds(),
		PayloadDurationSeconds:   state.PayloadDuration.Seconds(),
		SynRetransmissions:       state.SynRetransmissions,
		DataRetransmissions:      state.DataRetransmissions,
	}
}

func clientState(state model.ClientState) contract.ClientState {
	return contract.ClientState{
		Key:                      clientKey(state.Key),
		RetrySyn:                 state.RetrySyn,
		RetryData:                state.RetryData,
		AcknowledgedPayloadBytes: state.AcknowledgedPayloadBytes,
		Transfer:                 transferState(state.Transfer),
		Connection:               closeState(state.Connection),
		ECN:                      ecnState(state.ECN),
	}
}

func closeState(state model.CloseState) contract.CloseState {
	return contract.CloseState{
		State: string(state.State), Outcome: string(state.Outcome),
		FINRetransmissions: state.FINRetransmissions, Elapsed: state.Elapsed,
	}
}

func ecnState(state model.ECNState) *contract.ECNState {
	if !state.Enabled {
		return nil
	}
	return &contract.ECNState{
		Enabled:               state.Enabled,
		Negotiated:            state.Negotiated,
		ReceivedCEPacketCount: state.ReceivedCEPacketCount,
		SentECECount:          state.SentECECount,
		ReceivedECECount:      state.ReceivedECECount,
		SentCWRCount:          state.SentCWRCount,
		ECNResponseCount:      state.ECNResponseCount,
	}
}

// ackState retains counts for immediate and delayed ACK profiles alike.
func ackState(state model.ACKState) *contract.ACKState {
	if !state.Enabled && state.SentACKCount == 0 {
		return nil
	}
	return &contract.ACKState{
		Enabled:                state.Enabled,
		SentACKCount:           state.SentACKCount,
		TimerTriggeredACKCount: state.TimerTriggeredACKCount,
	}
}

// rtoState makes time units explicit at the report boundary.
func rtoState(state model.RTOState) contract.RTOState {
	return contract.RTOState{
		LatestRTTSeconds:    state.LatestRTT.Seconds(),
		SmoothedRTTSeconds:  state.SmoothedRTT.Seconds(),
		RTTVariationSeconds: state.RTTVariation.Seconds(),
		BaseRTOSeconds:      state.BaseRTO.Seconds(),
		CurrentRTOSeconds:   state.CurrentRTO.Seconds(),
		Samples:             state.Samples,
	}
}

// congestionState copies transport counters without exposing the mutable sender.
func congestionState(state model.CongestionState) contract.CongestionState {
	return contract.CongestionState{
		Mode:                    string(state.Mode),
		CongestionWindowBytes:   state.CongestionWindowBytes,
		SlowStartThresholdBytes: state.SlowStartThresholdBytes,
		BytesInFlight:           state.BytesInFlight,
		Phase:                   state.Phase,
		DuplicateACKs:           state.DuplicateACKs,
		FastRetransmissions:     state.FastRetransmissions,
	}
}

func serverState(state model.ServerState) contract.ServerState {
	return contract.ServerState{
		ID:                     state.ID,
		Name:                   state.Name,
		Address:                state.Address,
		Port:                   state.Port,
		Active:                 state.Active,
		ActiveConnections:      state.ActiveConnections,
		HalfOpenConnections:    state.HalfOpenConnections,
		LatencyP95Milli:        state.LatencyP95Milli,
		NumberOfHandledPackets: state.NumberOfHandledPackets,
		ReceivedPayloadBytes:   state.ReceivedPayloadBytes,
		CpuUsage:               float32(state.CpuUsage),
		Host:                   hostState(state.Host),
		ECN:                    ecnState(state.ECN),
		ACK:                    ackState(state.ACK),
	}
}

// AlgorithmLabel preserves the existing public implementation names while the
// native world keeps the canonical scenario/operation names.
func AlgorithmLabel(algorithm string) string {
	if algorithm == "random" {
		return "Random"
	}
	return "RoundRobin"
}

func newTransferSummary(info simulation.Info) contract.TransferSummary {
	return contract.TransferSummary{
		Started:    info.CreatedClients,
		NotStarted: info.ScheduledClients - info.CreatedClients,
		Failures:   make(map[contract.TransferFailureReason]uint64),
	}
}

// addDuration preserves the historical incremental mean's order of operations.
func addDuration(summary *contract.DurationSummary, seconds float64) {
	summary.Samples++
	summary.MeanSeconds += (seconds - summary.MeanSeconds) / float64(summary.Samples)
	if summary.Samples == 1 || seconds < summary.MinSeconds {
		summary.MinSeconds = seconds
	}
	if seconds > summary.MaxSeconds {
		summary.MaxSeconds = seconds
	}
}

func addTransfer(summary *contract.TransferSummary, transfer model.TransferState) {
	summary.SynRetransmissions += transfer.SynRetransmissions
	summary.DataRetransmissions += transfer.DataRetransmissions
	if transfer.HandshakeCompleted {
		addDuration(&summary.Handshake, transfer.HandshakeDuration.Seconds())
	}
	switch transfer.Status {
	case model.TransferSucceeded:
		summary.Succeeded++
		addDuration(&summary.Payload, transfer.PayloadDuration.Seconds())
		addDuration(&summary.Completion, transfer.Elapsed.Seconds())
	case model.TransferFailed:
		summary.Failed++
		summary.Failures[contract.TransferFailureReason(transfer.FailureReason)]++
	default:
		summary.InProgress++
	}
}

// TransferResults includes retained closed and unrouted attempts. Reaching the
// experiment horizon alone does not fail an unfinished transfer.
func TransferResults(world *simulation.World) contract.TransferSummary {
	summary := newTransferSummary(world.Info())
	world.VisitClients(func(client simulation.ClientObservation) { addTransfer(&summary, client.State.Transfer) })
	return summary
}

// MeasurePerformance measures wall execution separately from virtual traffic.
func MeasurePerformance(elapsed time.Duration, events, packets uint64) *contract.SimulationPerformance {
	measurement := &contract.SimulationPerformance{
		EventsExecuted:   events,
		ForwardedPackets: packets,
	}
	if elapsed > 0 {
		measurement.WallTimeSeconds = elapsed.Seconds()
		measurement.EventsPerWallSecond = float64(events) / elapsed.Seconds()
		measurement.ForwardedPacketsPerWallSecond = float64(packets) / elapsed.Seconds()
	}
	return measurement
}

func reportConfig(config network.Config) contract.LinkConfig {
	return contract.LinkConfig{
		BandwidthBitsPerSecond:      config.BandwidthBitsPerSecond,
		PacketTravelTime:            config.PacketTravelTime,
		QueueCapacity:               config.QueueCapacity,
		LossRate:                    config.LossRate,
		QueuePolicy:                 config.QueuePolicy,
		CongestionThresholdPercent:  config.CongestionThresholdPercent,
		CongestionSignalProbability: config.CongestionSignalProbability,
	}
}

func reportStats(stats network.Stats) contract.LinkStats {
	return contract.LinkStats{
		OfferedPackets:           stats.OfferedPackets,
		OfferedBytes:             stats.OfferedBytes,
		AcceptedPackets:          stats.AcceptedPackets,
		AcceptedBytes:            stats.AcceptedBytes,
		QueueDroppedPackets:      stats.QueueDroppedPackets,
		QueueDroppedBytes:        stats.QueueDroppedBytes,
		ECNMarkedPackets:         stats.ECNMarkedPackets,
		ECNMarkedBytes:           stats.ECNMarkedBytes,
		CongestionDroppedPackets: stats.CongestionDroppedPackets,
		CongestionDroppedBytes:   stats.CongestionDroppedBytes,
		TransmittedBytes:         stats.TransmittedBytes,
		LostPackets:              stats.LostPackets,
		LostBytes:                stats.LostBytes,
		DeliveredPackets:         stats.DeliveredPackets,
		DeliveredBytes:           stats.DeliveredBytes,
		QueuedPackets:            stats.QueuedPackets,
		Transmitting:             stats.Transmitting,
		TravellingPackets:        stats.TravellingPackets,
	}
}

func reportForwarding(stats network.ForwardingStats) contract.ForwardingStats {
	return contract.ForwardingStats{
		ForwardingBitsPerSecond:  stats.ForwardingBitsPerSecond,
		ForwardedPackets:         stats.ForwardedPackets,
		ForwardedBytes:           stats.ForwardedBytes,
		QueuedPackets:            stats.QueuedPackets,
		QueueDroppedPackets:      stats.QueueDroppedPackets,
		QueueDroppedBytes:        stats.QueueDroppedBytes,
		ECNMarkedPackets:         stats.ECNMarkedPackets,
		ECNMarkedBytes:           stats.ECNMarkedBytes,
		CongestionDroppedPackets: stats.CongestionDroppedPackets,
		CongestionDroppedBytes:   stats.CongestionDroppedBytes,
		BudgetWait:               stats.BudgetWait,
		BudgetBlocked:            stats.BudgetBlocked,
	}
}

// BuildResult retains the CLI report's format and all historical link counters.
// It shares measurements and aggregates with Sample without building topology.
func BuildResult(world *simulation.World, elapsed time.Duration) contract.SimulationResult {
	info := world.Info()
	ingress := world.Ingress()
	result := contract.SimulationResult{
		LoadBalancerHost: hostState(info.LoadBalancerHost),
		VirtualTime:      info.Now.String(),
		WallTime:         elapsed.String(),
		Seed:             info.Seed,
		EffectiveClose: contract.CloseConfig{
			MSL: info.Close.MSL, FinWaitTimeout: info.Close.FinWaitTimeout, MaxFINRetries: info.Close.MaxFINRetries,
		},
		TCP: contract.TCPConfig{
			ReceiveBufferBytes: info.ReceiveBufferBytes,
			MSSBytes:           info.MSSBytes,
			SACKEnabled:        info.SACKEnabled,
			ECNEnabled:         info.ECNEnabled,
			DelayedACK: contract.DelayedACKConfig{
				Enabled:        info.DelayedACK.Enabled,
				SegmentsPerACK: info.DelayedACK.SegmentsPerACK,
				MaxDelay:       info.DelayedACK.MaxDelay,
			},
			CongestionControl: contract.CongestionConfig{
				Mode:                           string(info.CongestionControl.Mode),
				FastRetransmit:                 info.CongestionControl.FastRetransmit,
				CubicFastConvergence:           info.CongestionControl.CubicFastConvergence,
				InitialWindowSegments:          info.CongestionControl.InitialWindowSegments,
				MaximumWindowSegments:          info.CongestionControl.MaximumWindowSegments,
				InitialSlowStartThresholdBytes: info.CongestionControl.InitialSlowStartThresholdBytes,
			},
			RTO: contract.RTOConfig{
				Mode:                  string(info.RTO.Mode),
				ExponentialBackoff:    info.RTO.ExponentialBackoff,
				InitialTimeoutSeconds: info.RTO.InitialTimeout.Seconds(),
				MinimumTimeoutSeconds: info.RTO.MinimumTimeout.Seconds(),
				MaximumTimeoutSeconds: info.RTO.MaximumTimeout.Seconds(),
			},
			ClientIdleTimeoutSeconds:             info.ClientIdleTimeout.Seconds(),
			BackendHalfOpenIdleTimeoutSeconds:    info.BackendHalfOpenIdleTimeout.Seconds(),
			BackendEstablishedIdleTimeoutSeconds: info.BackendEstablishedIdleTimeout.Seconds(),
		},
		LBIdleTimeouts: contract.LBIdleTimeoutConfig{
			HalfOpenIdleTimeoutSeconds:    info.LBHalfOpenIdleTimeout.Seconds(),
			EstablishedIdleTimeoutSeconds: info.LBEstablishedIdleTimeout.Seconds(),
			ClosingIdleTimeoutSeconds:     info.LBClosingIdleTimeout.Seconds(),
			ResetIdleTimeoutSeconds:       info.LBResetIdleTimeout.Seconds(),
		},
		ClientTimings:            make([]contract.ClientTiming, 0, info.CreatedClients),
		ClientCongestion:         make([]contract.ClientCongestion, 0, info.CreatedClients),
		ClientConnection:         make([]contract.ClientConnection, 0, info.CreatedClients),
		ScheduledClients:         info.ScheduledClients,
		CreatedClients:           info.CreatedClients,
		CompletedStreams:         info.CompletedStreams,
		ClosedClients:            info.ClosedClients,
		IncompleteStreams:        info.CreatedClients - info.CompletedStreams,
		CompletedPayloadBytes:    info.CompletedPayloadBytes,
		ReceivedPayloadBytes:     info.Payload.ReceivedBytes,
		AcknowledgedPayloadBytes: info.Payload.AcknowledgedBytes,
		Transfers:                newTransferSummary(info),
		EventsExecuted:           info.EventsExecuted,
		PendingEvents:            info.PendingEvents,
		Ingress: contract.LinkResult{
			Name:      "Shared LB input",
			Config:    reportConfig(ingress.Config),
			LinkStats: reportStats(ingress.Stats),
		},
		IngressQueues: make([]contract.IngressQueueResult, 0, len(ingress.Inputs)),
		Forwarding:    reportForwarding(info.Forwarding),
		Links:         make([]contract.LinkResult, 0),
		State: contract.LoadBalancerState{
			Mode:      contract.LBMode(info.Mode),
			Algorithm: AlgorithmLabel(info.Algorithm),
			Servers:   make([]contract.ServerStateWithClients, 0),
		},
	}
	for _, input := range ingress.Inputs {
		result.IngressQueues = append(result.IngressQueues, contract.IngressQueueResult{
			Side:      input.Side,
			Capacity:  input.Capacity,
			LinkStats: reportStats(input.Stats),
		})
	}
	world.VisitLinks(func(link simulation.LinkObservation) {
		result.Links = append(result.Links, contract.LinkResult{
			Name:      linkLabel(link),
			Config:    reportConfig(link.Config),
			LinkStats: reportStats(link.Stats),
		})
	})
	serverIndex := make(map[string]int)
	world.VisitBackends(func(backend simulation.BackendObservation) {
		serverIndex[backend.State.ID] = len(result.State.Servers)
		result.State.Servers = append(result.State.Servers, contract.ServerStateWithClients{
			Server:  serverState(backend.State),
			Clients: make([]contract.ClientState, 0),
		})
	})
	slots := make(map[model.ClientKey]routeSlot)
	world.VisitRoutes(func(route simulation.RouteObservation) {
		server := serverIndex[route.BackendID]
		slots[route.Key] = routeSlot{server, len(result.State.Servers[server].Clients)}
		result.State.Servers[server].Clients = append(result.State.Servers[server].Clients, contract.ClientState{})
	})
	world.VisitClients(func(client simulation.ClientObservation) {
		if state := ecnState(client.State.ECN); state != nil {
			result.ClientECN = append(result.ClientECN, contract.ClientECN{ClientID: client.ID, State: *state})
		}
		addTransfer(&result.Transfers, client.State.Transfer)

		// Retain completed and unrouted clients as well as current LB routes.
		result.ClientTimings = append(result.ClientTimings, contract.ClientTiming{
			ClientID: client.ID,
			RTO:      rtoState(client.State.RTO),
		})
		result.ClientCongestion = append(result.ClientCongestion, contract.ClientCongestion{
			ClientID: client.ID,
			State:    congestionState(client.State.Congestion),
		})
		result.ClientConnection = append(result.ClientConnection, contract.ClientConnection{
			ClientID: client.ID, State: closeState(client.State.Connection),
		})
		if slot, routed := slots[client.State.Key]; routed {
			result.State.Servers[slot.server].Clients[slot.client] = clientState(client.State)
		}
	})
	return result
}
