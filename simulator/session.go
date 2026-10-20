package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	publish := func() error {
		state := model.LoadBalancerState{Mode: model.LBModeNAT, Algorithm: "RoundRobin", Servers: []model.ServerStateWithClients{}}
		if config.Algorithm == "random" {
			state.Algorithm = "Random"
		}
		virtualTime := 0.0
		topology := model.Topology{Nodes: []model.TopologyNode{}, Links: []model.TopologyLink{}}
		if current != nil {
			state = current.lb.Tick()
			virtualTime = current.engine.Now().Seconds()
			topology = current.topology(state)
		}
		return hub.BroadcastState(simulationSnapshot{
			LoadBalancerState: state,
			Simulation:        simulationMetadata{Status: status, Config: config, VirtualTimeSeconds: virtualTime, Error: lastError},
			Topology:          topology,
		})
	}
	if err := publish(); err != nil {
		return err
	}
	clear := func() error {
		// Discard the whole model, including event callbacks, link queues and TCP state.
		current = nil
		status, lastError = "idle", ""
		control.finishRun()
		err := control.acknowledgeStop(publish)
		log.Print("Simulation stopped and cleared; waiting for another start")
		return err
	}

	for {
		var command httpserver.Envelope
		select {
		case <-ctx.Done():
			return nil
		case <-control.stops:
			if err := clear(); err != nil {
				return err
			}
			continue
		case command = <-control.commands:
		}
		if command.Type != httpserver.StartSimulation {
			lastError = "start the simulation before sending a command"
			if err := publish(); err != nil {
				return err
			}
			continue
		}
		runCtx := control.contextForRun()
		if runCtx.Err() != nil {
			if err := clear(); err != nil {
				return err
			}
			continue
		}

		// Missing fields inherit the Go defaults; unknown or malformed fields fail.
		requested := defaults.publicConfig()
		decoder := json.NewDecoder(bytes.NewReader(command.Payload))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&requested)
		var cfg configuration
		if err == nil && bytes.Equal(bytes.TrimSpace(command.Payload), []byte("null")) {
			err = errors.New("configuration must be a JSON object")
		}
		if err == nil {
			cfg, err = requested.configuration(defaults)
		}
		if err != nil {
			lastError = fmt.Sprintf("invalid configuration: %v", err)
			control.finishRun()
			if err := publish(); err != nil {
				return err
			}
			continue
		}

		config, status, lastError = requested, "running", ""
		current = newSimulation(cfg)
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
		control.finishRun()
		if err := publish(); err != nil {
			return err
		}
		log.Printf("Simulation %s; waiting for another start", status)
	}
}
