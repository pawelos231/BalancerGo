package main

import (
	"context"
	"errors"
	"fmt"
	httpserver "load-balancer/http"
	"load-balancer/scenario"
	"strconv"
	"sync"
)

// startRequest carries a checked HTTP scenario or the original simple WS settings.
// Only the simulation owner creates the model from this request.
type startRequest struct {
	runID    string
	legacy   *httpserver.Envelope
	scenario *scenario.Normalized
}

// simulationControl coordinates I/O requests without accessing live models.
type simulationControl struct {
	mu         sync.Mutex
	commands   chan httpserver.Envelope
	starts     chan startRequest
	stops      chan struct{}
	serviceCtx context.Context
	nextRunID  uint64
	current    *httpserver.RunRecord
	runCtx     context.Context
	cancelRun  context.CancelFunc
	stopping   bool
}

func newSimulationControl(ctx context.Context) *simulationControl {
	return &simulationControl{
		commands:   make(chan httpserver.Envelope, 64),
		starts:     make(chan startRequest, 1),
		stops:      make(chan struct{}, 1),
		serviceCtx: ctx,
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
		_, err := c.enqueueStart(startRequest{legacy: &command}, nil)
		return err
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
		return httpserver.ErrCommandQueueFull
	}
}

// startScenario reserves the same start slot as WS, independent of request lifetime.
func (c *simulationControl) startScenario(
	ctx context.Context,
	document []byte,
	resolved scenario.Normalized,
) (httpserver.RunReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return httpserver.RunReceipt{}, err
	}
	return c.enqueueStart(startRequest{scenario: &resolved}, document)
}

// enqueueStart requires the control lock; all transports use this admission check.
func (c *simulationControl) enqueueStart(request startRequest, document []byte) (httpserver.RunReceipt, error) {
	if c.serviceCtx.Err() != nil {
		return httpserver.RunReceipt{}, httpserver.ErrServiceClosed
	}
	if c.cancelRun != nil || c.stopping {
		return httpserver.RunReceipt{}, httpserver.ErrSimulationBusy
	}
	if c.nextRunID == ^uint64(0) {
		return httpserver.RunReceipt{}, errors.New("simulation run ID limit reached")
	}

	runID := "run-" + strconv.FormatUint(c.nextRunID+1, 10)
	request.runID = runID
	c.runCtx, c.cancelRun = context.WithCancel(c.serviceCtx)
	select {
	case c.starts <- request:
		c.nextRunID++
		if document != nil {
			c.current = &httpserver.RunRecord{
				RunID:    runID,
				Status:   "accepted",
				Scenario: append([]byte(nil), document...),
			}
		}
		return httpserver.RunReceipt{RunID: runID, Status: "accepted"}, nil
	default:
		c.cancelRun()
		c.runCtx, c.cancelRun = nil, nil
		return httpserver.RunReceipt{}, httpserver.ErrCommandQueueFull
	}
}

// rememberRun records a validated legacy start without exposing model references.
func (c *simulationControl) rememberRun(runID string, document []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = &httpserver.RunRecord{
		RunID:    runID,
		Status:   "running",
		Scenario: append([]byte(nil), document...),
	}
}

// setRunStatus updates lifecycle metadata; the scenario remains the initial document.
func (c *simulationControl) setRunStatus(runID, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil && c.current.RunID == runID {
		c.current.Status = status
	}
}

// currentRun returns a detached record for HTTP readers, never the live simulation.
func (c *simulationControl) currentRun() *httpserver.RunRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return nil
	}
	record := *c.current
	record.Scenario = append([]byte(nil), record.Scenario...)
	return &record
}

// forgetRun removes configuration metadata when Stop clears the experiment.
func (c *simulationControl) forgetRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = nil
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
	for len(c.starts) > 0 {
		<-c.starts
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
