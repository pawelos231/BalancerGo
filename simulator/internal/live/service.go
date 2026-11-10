// Package live coordinates interactive runs and their HTTP/WebSocket publication.
package live

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

// Serve starts the HTTP service and owns interactive runs until ctx is canceled.
func Serve(
	ctx context.Context,
	stop context.CancelFunc,
	defaults func() scenario.Scenario,
	initial contract.SimulationConfig,
) error {
	// The I/O queue carries requests; only runInteractive owns the model state.
	control := newRunController(ctx)
	publication := &httpserver.Publication{}
	hub := httpserver.NewHub(func(command contract.Envelope, reply func(contract.CommandResultFrame)) error {
		return submitFrontendCommand(control, ctx, command, reply)
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
	return runInteractive(ctx, initial, hub, control, publication)
}
