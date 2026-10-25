package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/trace"
	"syscall"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	defaults, err := readConfiguration()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Full JSON scenarios use a standalone run and never start the HTTP service.
	if defaults.scenarioPath != "" {
		return runScenarioFile(ctx, defaults, os.Stdout)
	}

	if defaults.tracePath != "" {
		file, err := os.Create(defaults.tracePath)
		if err != nil {
			return err
		}
		if err := trace.Start(file); err != nil {
			file.Close()
			return err
		}
		defer file.Close()
		defer trace.Stop()
	}

	// Headless runs remain explicit CLI experiments without a browser.
	if defaults.headless {
		cfg, err := defaults.normalized()
		if err != nil {
			return err
		}
		resolved, err := scenario.Normalize(cfg.scenarioDefinition())
		if err != nil {
			return err
		}
		sim := simulation.New(resolved)
		started := time.Now()
		err = runWorld(ctx, sim, nil, nil, nil)
		result := presentation.BuildResult(sim, time.Since(started))
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return encodeErr
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	// The I/O queue carries requests; only runInteractive owns the model state.
	control := newRunController(ctx)
	publication := &httpserver.Publication{}
	hub := httpserver.NewHub(func(command contract.Envelope) error {
		return submitFrontendCommand(control, ctx, command)
	})
	go hub.Run(ctx)
	api := &scenarioAPI{defaults: defaults, control: control, publication: publication}
	srv := httpserver.NewServer(hub, os.Getenv("LB_HTTP_ADDR"), api)
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server: %v", err)
			stop()
		}
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("WebSocket server listening on %s/ws; waiting for simulation start", srv.Addr)
	return runInteractive(ctx, defaults, hub, control, publication)
}
