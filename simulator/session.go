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
	Status             string           `json:"status"`
	RunID              string           `json:"runId,omitempty"`
	Config             simulationConfig `json:"config"`
	VirtualTimeSeconds float64          `json:"virtualTimeSeconds"`
	Error              string           `json:"error,omitempty"`
}

type simulationSnapshot struct {
	model.LoadBalancerState
	Simulation simulationMetadata `json:"simulation"`
	Topology   model.Topology     `json:"topology"`
}

// runInteractive waits for a validated start request before creating a model.
// Subsequent experiments use fresh engines, endpoints, links and random sources.
func runInteractive(ctx context.Context, defaults configuration, hub *httpserver.Hub, control *simulationControl) error {
	var current *simulation
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
		topology := model.Topology{Nodes: []model.TopologyNode{}, Links: []model.TopologyLink{}}
		if current != nil {
			state = current.lb.Tick()
			// The small legacy summary follows runtime algorithm changes too.
			config.Algorithm = "roundRobin"
			if state.Algorithm == "Random" {
				config.Algorithm = "random"
			}
			virtualTime = current.engine.Now().Seconds()
			topology = current.topology(state)
		}
		return hub.BroadcastState(simulationSnapshot{
			LoadBalancerState: state,
			Simulation: simulationMetadata{
				Status:             status,
				RunID:              runID,
				Config:             config,
				VirtualTimeSeconds: virtualTime,
				Error:              lastError,
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
		current = newScenarioSimulation(prepared.scenario)
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
		started := time.Now()
		err = current.run(runCtx, control.commands)
		if ctx.Err() != nil {
			return nil
		}
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(current.result(time.Since(started))); encodeErr != nil {
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
