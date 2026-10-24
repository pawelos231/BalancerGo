package main

import (
	"context"
	"encoding/json"
	"errors"
	httpserver "load-balancer/http"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/trace"
	"syscall"
	"time"
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
		sim := newSimulation(cfg)
		started := time.Now()
		err = sim.run(ctx, nil)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(sim.result(time.Since(started))); encodeErr != nil {
			return encodeErr
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	// The I/O queue carries requests; only runInteractive owns the model state.
	control := newSimulationControl(ctx)
	hub := httpserver.NewHub(func(command httpserver.Envelope) error {
		return control.submit(ctx, command)
	})
	go hub.Run(ctx)
	api := &scenarioAPI{defaults: defaults, control: control}
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
	return runInteractive(ctx, defaults, hub, control)
}
