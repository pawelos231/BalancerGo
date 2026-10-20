package main

import (
	"context"
	"errors"
	"fmt"
	httpserver "load-balancer/http"
	"sync"
)

// simulationControl coordinates I/O requests without accessing live models.
type simulationControl struct {
	mu        sync.Mutex
	commands  chan httpserver.Envelope
	stops     chan struct{}
	runCtx    context.Context
	cancelRun context.CancelFunc
	stopping  bool
}

func newSimulationControl() *simulationControl {
	return &simulationControl{
		commands: make(chan httpserver.Envelope, 64),
		stops:    make(chan struct{}, 1),
	}
}

// submit checks frontend commands and queues Start/runtime commands for the
// simulation owner. Stop cancels the run and signals the owner to clear its state.
func (c *simulationControl) submit(ctx context.Context, command httpserver.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	switch command.Type {
	case httpserver.StopSimulation:
		if !c.stopping {
			c.stopping = true
			if c.cancelRun != nil {
				c.cancelRun()
			}
			// Stop bypasses the command queue and interrupts pacing immediately.
			c.stops <- struct{}{}
		}
		return nil
	case httpserver.StartSimulation:
		if c.cancelRun != nil || c.stopping {
			return errors.New("simulation is already running or a start/stop request is pending")
		}
		c.runCtx, c.cancelRun = context.WithCancel(ctx)
	case httpserver.SetAlgorithm, httpserver.SpawnClient:
		if c.cancelRun == nil || c.stopping {
			return errors.New("start the simulation before sending a command")
		}
	default:
		return fmt.Errorf("unsupported command: %s", command.Type)
	}

	select {
	case c.commands <- command:
		return nil
	default:
		if command.Type == httpserver.StartSimulation {
			c.cancelRun()
			c.runCtx, c.cancelRun = nil, nil
		}
		return errors.New("simulation command queue is full")
	}
}

func (c *simulationControl) contextForRun() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runCtx
}

func (c *simulationControl) finishRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelRun != nil {
		c.cancelRun()
	}
	c.runCtx, c.cancelRun = nil, nil
	// Submission uses the same lock, so old commands cannot enter the next run.
	for len(c.commands) > 0 {
		<-c.commands
	}
}

// acknowledgeStop publishes the empty state before allowing new starts.
func (c *simulationControl) acknowledgeStop(publish func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.stops:
	default:
	}
	err := publish()
	c.stopping = false
	return err
}
