package live

import (
	"context"
	"encoding/json"
	"errors"
	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
	"time"
)

// scenarioAPI connects HTTP to admission control without accessing live models.
// Defaults and run records are configuration documents, independent of snapshots.
type scenarioAPI struct {
	defaults    func() scenario.Scenario
	control     *runController
	publication *httpserver.Publication
}

// preparedStart keeps the checked plan with its read-only run summary.
type preparedStart struct {
	plan     *scenario.Compiled
	config   contract.SimulationConfig
	document []byte
}

func (api *scenarioAPI) Defaults() scenario.Scenario {
	return api.defaults()
}

// StartScenario retains the complete document admitted by the HTTP compiler.
func (api *scenarioAPI) StartScenario(
	ctx context.Context,
	input scenario.Scenario,
	plan *scenario.Compiled,
) (contract.RunReceipt, error) {
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		return contract.RunReceipt{}, err
	}
	receipt, err := api.control.startTimeline(ctx, document, plan)
	return contract.RunReceipt{RunID: receipt.RunID, Status: receipt.Status}, admissionError(err)
}

// CurrentRun returns initial settings and lifecycle metadata, including finished runs.
func (api *scenarioAPI) CurrentRun() *contract.RunRecord {
	record := api.control.currentRun()
	if record == nil {
		return nil
	}
	return &contract.RunRecord{
		RunID:      record.RunID,
		Status:     record.Status,
		Scenario:   record.Scenario,
		Experiment: record.Experiment,
	}
}

// CurrentTopology serves the last published graph, including finished experiments.
func (api *scenarioAPI) CurrentTopology() json.RawMessage {
	return api.publication.CurrentTopology()
}

// prepareStart validates a WebSocket scenario document or uses an admitted HTTP plan.
func prepareStart(request startRequest) (preparedStart, error) {
	if request.plan != nil {
		return preparedStart{plan: request.plan, config: scenarioSummary(request.plan)}, nil
	}
	if request.command == nil {
		return preparedStart{}, errors.New("compiled scenario or scenario document is required")
	}
	input, err := scenario.DecodeDocument(request.command.Payload)
	if err != nil {
		return preparedStart{}, err
	}
	plan, err := scenario.Compile(input)
	if err != nil {
		return preparedStart{}, err
	}
	document, err := scenario.EncodeDocument(input)
	return preparedStart{plan: plan, config: scenarioSummary(plan), document: document}, err
}

// Telemetry includes an initial summary; the run record retains every state.
func scenarioSummary(plan *scenario.Compiled) contract.SimulationConfig {
	fixed := plan.Fixed()
	initial, _ := plan.StateAt(0)
	execution := fixed.Execution
	links := fixed.NetworkDefaults
	stream := fixed.DefaultStream
	ingress := initial.LoadBalancer.Ingress
	lbHost := initial.LoadBalancer.Host
	backendHost := scenario.DefaultHostConfig()
	if len(initial.Backends) > 0 {
		// The scalar summary uses the first backend; the scenario retains all overrides.
		backendHost = initial.Backends[0].Host
	}
	return contract.SimulationConfig{
		Clients:                            plan.TotalClients(),
		Servers:                            len(initial.Backends),
		Packets:                            stream.Packets,
		Bytes:                              stream.Bytes,
		MSSBytes:                           fixed.TCP.MSSBytes,
		ReceiveBufferBytes:                 fixed.TCP.ReceiveBufferBytes,
		SACKEnabled:                        fixed.TCP.SACKEnabled,
		ECNEnabled:                         fixed.TCP.ECNEnabled,
		DelayedACKEnabled:                  fixed.TCP.DelayedACK.Enabled,
		DelayedACKSegments:                 fixed.TCP.DelayedACK.SegmentsPerACK,
		DelayedACKMaxDelayMs:               float64(fixed.TCP.DelayedACK.MaxDelay) / float64(time.Millisecond),
		CongestionControl:                  fixed.TCP.CongestionControl.Mode,
		FastRetransmit:                     fixed.TCP.CongestionControl.FastRetransmit,
		CubicFastConvergence:               fixed.TCP.CongestionControl.CubicFastConvergence,
		InitialCongestionWindowSegments:    fixed.TCP.CongestionControl.InitialWindowSegments,
		MaximumCongestionWindowSegments:    fixed.TCP.CongestionControl.MaximumWindowSegments,
		InitialSlowStartThresholdBytes:     fixed.TCP.CongestionControl.InitialSlowStartThresholdBytes,
		RTOMode:                            string(fixed.TCP.RTO.Mode),
		RTOBackoff:                         fixed.TCP.RTO.ExponentialBackoff,
		RTOInitialMs:                       float64(fixed.TCP.RTO.InitialTimeout) / float64(time.Millisecond),
		RTOMinimumMs:                       float64(fixed.TCP.RTO.MinimumTimeout) / float64(time.Millisecond),
		RTOMaximumMs:                       float64(fixed.TCP.RTO.MaximumTimeout) / float64(time.Millisecond),
		TCPMSLMs:                           float64(fixed.TCP.Close.MSL) / float64(time.Millisecond),
		TCPFinWaitTimeoutMs:                float64(fixed.TCP.Close.FinWaitTimeout) / float64(time.Millisecond),
		TCPMaxFINRetries:                   fixed.TCP.Close.MaxFINRetries,
		ClientIdleTimeoutMs:                float64(fixed.TCP.ClientIdleTimeout) / float64(time.Millisecond),
		BackendHalfOpenIdleTimeoutMs:       float64(fixed.TCP.BackendHalfOpenIdleTimeout) / float64(time.Millisecond),
		BackendEstablishedIdleTimeoutMs:    float64(fixed.TCP.BackendEstablishedIdleTimeout) / float64(time.Millisecond),
		LBHalfOpenIdleTimeoutMs:            float64(initial.LoadBalancer.HalfOpenIdleTimeout) / float64(time.Millisecond),
		LBEstablishedIdleTimeoutMs:         float64(initial.LoadBalancer.EstablishedIdleTimeout) / float64(time.Millisecond),
		LBClosingIdleTimeoutMs:             float64(initial.LoadBalancer.ClosingIdleTimeout) / float64(time.Millisecond),
		LBResetIdleTimeoutMs:               float64(initial.LoadBalancer.ResetIdleTimeout) / float64(time.Millisecond),
		LBHostCores:                        lbHost.Cores,
		LBPacketProcessingTimeUs:           float64(lbHost.PacketProcessingTime) / float64(time.Microsecond),
		LBCPUQueueCapacity:                 lbHost.QueueCapacity,
		BackendHostCores:                   backendHost.Cores,
		BackendPacketProcessingTimeUs:      float64(backendHost.PacketProcessingTime) / float64(time.Microsecond),
		BackendCPUQueueCapacity:            backendHost.QueueCapacity,
		DurationSeconds:                    execution.Duration.Seconds(),
		SnapshotIntervalMs:                 float64(execution.SnapshotInterval) / float64(time.Millisecond),
		PacketGapMs:                        float64(stream.PacketGap) / float64(time.Millisecond),
		BandwidthBitsPerSecond:             links.BandwidthBitsPerSecond,
		ForwardingBitsPerSecond:            initial.LoadBalancer.ForwardingBitsPerSecond,
		PacketTravelTimeMs:                 float64(links.PacketTravelTime) / float64(time.Millisecond),
		LinkQueue:                          links.QueueCapacity,
		Loss:                               links.LossRate,
		QueuePolicy:                        links.QueuePolicy,
		CongestionThresholdPercent:         links.CongestionThresholdPercent,
		CongestionSignalProbability:        links.CongestionSignalProbability,
		IngressBandwidthBitsPerSecond:      ingress.BandwidthBitsPerSecond,
		IngressPacketTravelTimeMs:          float64(ingress.PacketTravelTime) / float64(time.Millisecond),
		IngressQueueCapacity:               ingress.QueueCapacity,
		IngressLoss:                        ingress.LossRate,
		IngressQueuePolicy:                 ingress.QueuePolicy,
		IngressCongestionThresholdPercent:  ingress.CongestionThresholdPercent,
		IngressCongestionSignalProbability: ingress.CongestionSignalProbability,
		Seed:                               execution.Seed,
		Algorithm:                          initial.LoadBalancer.Algorithm,
		Speed:                              execution.Speed,
		MaxEvents:                          execution.MaxEvents,
	}
}

func (prepared preparedStart) newWorld() (*simulation.World, error) {
	return simulation.NewTimeline(prepared.plan)
}
