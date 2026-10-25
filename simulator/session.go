package main

import (
	"context"
	"encoding/json"
	"fmt"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"log"
	"os"
	"time"
)

// simulationMetadata describes the browser-controlled experiment lifecycle.
type simulationMetadata struct {
	Status             string                 `json:"status"`
	RunID              string                 `json:"runId,omitempty"`
	Algorithm          string                 `json:"algorithm"`
	VirtualTimeSeconds float64                `json:"virtualTimeSeconds"`
	Error              string                 `json:"error,omitempty"`
	Performance        *simulationPerformance `json:"performance,omitempty"`
	Payload            *payloadProgress       `json:"payload,omitempty"`
	Transfers          *transferSummary       `json:"transfers,omitempty"`
}

type simulationSnapshot struct {
	Type       string                  `json:"type"`
	Version    int                     `json:"version"`
	Mode       model.LBMode            `json:"mode"`
	Algorithm  string                  `json:"algorithm"`
	Servers    []model.TelemetryServer `json:"servers"`
	Simulation simulationMetadata      `json:"simulation"`
	Topology   model.TelemetryTopology `json:"topology"`
}

// runInteractive waits for a validated start request before creating a model.
// Subsequent experiments use fresh engines, endpoints, links and random sources.
func runInteractive(ctx context.Context, defaults configuration, hub *httpserver.Hub, control *simulationControl) error {
	var current *simulation
	var telemetry *telemetryPublisher
	var started time.Time
	var elapsed time.Duration
	config := defaults.publicConfig()
	status := "idle"
	lastError := ""
	runID := ""

	publish := func() error {
		state := model.LoadBalancerState{Mode: model.LBModeNAT, Algorithm: "RoundRobin", Servers: []model.ServerStateWithClients{}}
		if config.Algorithm == "random" {
			state.Algorithm = "Random"
		}
		virtualTime := 0.0
		topology := model.TelemetryTopology{
			Revision: "0",
			Nodes:    []model.TelemetryNode{},
			Links:    []model.TelemetryLink{},
		}
		servers := []model.TelemetryServer{}
		var performance *simulationPerformance
		var payload *payloadProgress
		var transfers *transferSummary
		if current != nil {
			// Copy totals so a published snapshot keeps its values as the run advances.
			progress := current.payload
			payload = &progress
			state = current.lb.Tick()
			// The small legacy summary follows runtime algorithm changes too.
			config.Algorithm = "roundRobin"
			if state.Algorithm == "Random" {
				config.Algorithm = "random"
			}
			virtualTime = current.engine.Now().Seconds()
			// The definition is replaced only when nodes or links change.
			sample, err := telemetry.sample(current, state)
			if err != nil {
				return err
			}
			if sample.Document != nil {
				if err := control.publishTopology(sample.Document); err != nil {
					return err
				}
			}
			topology, servers = sample.Topology, sample.Servers
			transfers = &sample.Transfers
			wallTime := elapsed
			if status == "running" {
				wallTime = time.Since(started)
			}
			performance = measurePerformance(
				wallTime,
				current.engine.EventsExecuted(),
				current.egress.Stats().ForwardedPackets,
			)
		} else {
			control.clearTopology()
		}
		return hub.BroadcastState(simulationSnapshot{
			Type:      "simulationSnapshot",
			Version:   1,
			Mode:      state.Mode,
			Algorithm: state.Algorithm,
			Servers:   servers,
			Simulation: simulationMetadata{
				Status:             status,
				RunID:              runID,
				Algorithm:          config.Algorithm,
				VirtualTimeSeconds: virtualTime,
				Error:              lastError,
				Performance:        performance,
				Payload:            payload,
				Transfers:          transfers,
			},
			Topology: topology,
		})
	}
	if err := publish(); err != nil {
		return err
	}
	clear := func() error {
		// Discard the whole model, including event callbacks, link queues and TCP state.
		current = nil
		telemetry = nil
		started, elapsed = time.Time{}, 0
		status, lastError = "idle", ""
		runID = ""
		control.forgetRun()
		control.finishRun()
		err := control.acknowledgeStop(publish)
		log.Print("Simulation stopped and cleared; waiting for another start")
		return err
	}

	for {
		var request startRequest
		select {
		case <-ctx.Done():
			return nil
		case <-control.stops:
			if err := clear(); err != nil {
				return err
			}
			continue
		case request = <-control.starts:
		}
		runCtx := control.contextForRun()
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}

		// Both transports reach this owner before any engine or endpoint is created.
		prepared, err := prepareStart(request, defaults)
		if err != nil {
			lastError = fmt.Sprintf("invalid configuration: %v", err)
			control.finishRun()
			if err := publish(); err != nil {
				return err
			}
			continue
		}
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}

		config, status, lastError = prepared.config, "running", ""
		runID = request.runID
		if prepared.document != nil {
			control.rememberRun(runID, prepared.document)
		} else {
			control.setRunStatus(runID, status)
		}
		started, elapsed = time.Now(), 0
		current = newScenarioSimulation(prepared.scenario)
		telemetry = newTelemetryPublisher(runID)
		current.publishState = publish
		current.commandHandled = func(err error) {
			lastError = ""
			if err != nil {
				lastError = err.Error()
			}
			if err := publish(); err != nil {
				log.Printf("snapshot: %v", err)
			}
		}
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Starting simulation: %+v", config)
		err = current.run(runCtx, control.commands)
		elapsed = time.Since(started)
		if ctx.Err() != nil {
			return nil
		}
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(current.result(elapsed)); encodeErr != nil {
			return encodeErr
		}

		status = "finished"
		if err != nil {
			status, lastError = "failed", err.Error()
		}
		// Runtime errors are reported to the UI; the service can accept a new run.
		control.setRunStatus(runID, status)
		control.finishRun()
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Simulation %s; waiting for another start", status)
	}
}
