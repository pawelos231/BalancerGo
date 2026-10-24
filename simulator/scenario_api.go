package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	httpserver "load-balancer/http"
	"load-balancer/scenario"
	"time"
)

// scenarioAPI connects HTTP to admission control without accessing live models.
// Defaults and run records are configuration documents, independent of snapshots.
type scenarioAPI struct {
	defaults configuration
	control  *simulationControl
}

// preparedStart keeps runtime settings and the temporary form summary together.
// Document is set only when the legacy WS input still needs a stored scenario.
type preparedStart struct {
	scenario scenario.Normalized
	config   simulationConfig
	document []byte
}

// Defaults builds the same initial scenario used by the existing Go settings.
func (api *scenarioAPI) Defaults() scenario.Scenario {
	return api.defaults.scenarioDefinition()
}

// StartScenario queues a checked scenario; closing the HTTP request cannot stop it.
func (api *scenarioAPI) StartScenario(
	ctx context.Context,
	input scenario.Scenario,
	resolved scenario.Normalized,
) (httpserver.RunReceipt, error) {
	document, err := scenario.EncodeDocument(input)
	if err != nil {
		return httpserver.RunReceipt{}, err
	}
	return api.control.startScenario(ctx, document, resolved)
}

// CurrentRun returns initial settings and lifecycle metadata, including finished runs.
func (api *scenarioAPI) CurrentRun() *httpserver.RunRecord {
	return api.control.currentRun()
}

// prepareStart keeps WS settings compatible while HTTP supplies a checked scenario.
func prepareStart(request startRequest, defaults configuration) (preparedStart, error) {
	if request.scenario != nil {
		resolved := *request.scenario
		return preparedStart{scenario: resolved, config: scenarioSummary(resolved)}, nil
	}

	requested := defaults.publicConfig()
	decoder := json.NewDecoder(bytes.NewReader(request.legacy.Payload))
	decoder.DisallowUnknownFields()
	if bytes.Equal(bytes.TrimSpace(request.legacy.Payload), []byte("null")) {
		return preparedStart{}, errors.New("configuration must be a JSON object")
	}
	if err := decoder.Decode(&requested); err != nil {
		return preparedStart{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return preparedStart{}, errors.New("configuration must contain exactly one JSON object")
	}

	cfg, err := requested.configuration(defaults)
	if err != nil {
		return preparedStart{}, err
	}
	input := cfg.scenarioDefinition()
	input.Workload.Defaults.Bytes = requested.Bytes
	resolved, err := scenario.Normalize(input)
	if err != nil {
		return preparedStart{}, err
	}
	document, err := scenario.EncodeDocument(input)
	return preparedStart{scenario: resolved, config: requested, document: document}, err
}

// scenarioSummary adapts shared defaults for the old form during the HTTP migration.
// Profiles and overrides remain available in full through CurrentRun.
func scenarioSummary(resolved scenario.Normalized) simulationConfig {
	execution := resolved.Execution
	links := resolved.NetworkDefaults
	stream := resolved.DefaultStream
	ingress := resolved.LoadBalancer.Ingress
	return simulationConfig{
		Clients:                       resolved.TotalClients,
		Servers:                       len(resolved.Backends),
		Packets:                       stream.Packets,
		Bytes:                         stream.Bytes,
		DurationSeconds:               execution.Duration.Seconds(),
		SnapshotIntervalMs:            float64(execution.SnapshotInterval) / float64(time.Millisecond),
		PacketGapMs:                   float64(stream.PacketGap) / float64(time.Millisecond),
		BandwidthBitsPerSecond:        links.BandwidthBitsPerSecond,
		ForwardingBitsPerSecond:       resolved.LoadBalancer.ForwardingBitsPerSecond,
		PacketTravelTimeMs:            float64(links.PacketTravelTime) / float64(time.Millisecond),
		LinkQueue:                     links.QueueCapacity,
		Loss:                          links.LossRate,
		IngressBandwidthBitsPerSecond: ingress.BandwidthBitsPerSecond,
		IngressPacketTravelTimeMs:     float64(ingress.PacketTravelTime) / float64(time.Millisecond),
		IngressQueueCapacity:          ingress.QueueCapacity,
		IngressLoss:                   ingress.LossRate,
		Seed:                          execution.Seed,
		Algorithm:                     resolved.LoadBalancer.Algorithm,
		Speed:                         execution.Speed,
		MaxEvents:                     execution.MaxEvents,
	}
}
