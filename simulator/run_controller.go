package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"load-balancer/scenario"
)

// commandKind identifies an application action, independently of its HTTP/WS name.
type commandKind uint8

const (
	startCommand commandKind = iota
	stopCommand
	algorithmCommand
	spawnCommand
)

// runtimeCommand keeps decoding at the existing virtual boundary. Only the
// input adapter interprets Payload; World receives a typed operation.
type runtimeCommand struct {
	kind    commandKind
	Payload []byte
}

type runReceipt struct{ RunID, Status string }
type runRecord struct {
	RunID, Status string
	Scenario      []byte
}

var (
	errSimulationBusy   = errors.New("simulation is already running or a request is pending")
	errCommandQueueFull = errors.New("simulation command queue is full")
	errServiceClosed    = errors.New("simulation service is closed")
)

// startRequest carries a checked scenario or settings awaiting validation.
// Only the simulation owner creates the world from this request.
type startRequest struct {
	runID    string
	legacy   *runtimeCommand
	scenario *scenario.Normalized
}

// runController coordinates I/O requests without accessing live models.
type runController struct {
	mu         sync.Mutex
	commands   chan runtimeCommand
	starts     chan startRequest
	stops      chan struct{}
	serviceCtx context.Context
	nextRunID  uint64
	current    *runRecord
	runCtx     context.Context
	cancelRun  context.CancelFunc
	stopping   bool
}

func newRunController(ctx context.Context) *runController {
	return &runController{
		commands:   make(chan runtimeCommand, 64),
		starts:     make(chan startRequest, 1),
		stops:      make(chan struct{}, 1),
		serviceCtx: ctx,
	}
}

// submit checks application requests and queues Start/runtime commands for the
// simulation owner. Stop cancels the run and signals the owner to clear its state.
func (c *runController) submit(ctx context.Context, command runtimeCommand) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	switch command.kind {
	case stopCommand:
		if !c.stopping {
			c.stopping = true
			if c.cancelRun != nil {
				c.cancelRun()
			}
			// Stop bypasses the command queue and interrupts pacing immediately.
			c.stops <- struct{}{}
		}
		return nil
	case startCommand:
		_, err := c.enqueueStart(startRequest{legacy: &command}, nil)
		return err
	case algorithmCommand, spawnCommand:
		if c.cancelRun == nil || c.stopping {
			return errors.New("start the simulation before sending a command")
		}
	default:
		return fmt.Errorf("unsupported application command: %d", command.kind)
	}

	select {
	case c.commands <- command:
		return nil
	default:
		return errCommandQueueFull
	}
}

// startScenario reserves the same start slot as WS, independent of request lifetime.
func (c *runController) startScenario(
	ctx context.Context,
	document []byte,
	resolved scenario.Normalized,
) (runReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return runReceipt{}, err
	}
	return c.enqueueStart(startRequest{scenario: &resolved}, document)
}

// enqueueStart requires the control lock; all transports use this admission check.
func (c *runController) enqueueStart(request startRequest, document []byte) (runReceipt, error) {
	if c.serviceCtx.Err() != nil {
		return runReceipt{}, errServiceClosed
	}
	if c.cancelRun != nil || c.stopping {
		return runReceipt{}, errSimulationBusy
	}
	if c.nextRunID == ^uint64(0) {
		return runReceipt{}, errors.New("simulation run ID limit reached")
	}

	runID := "run-" + strconv.FormatUint(c.nextRunID+1, 10)
	request.runID = runID
	c.runCtx, c.cancelRun = context.WithCancel(c.serviceCtx)
	select {
	case c.starts <- request:
		c.nextRunID++
		if document != nil {
			c.current = &runRecord{
				RunID:    runID,
				Status:   "accepted",
				Scenario: append([]byte(nil), document...),
			}
		}
		return runReceipt{RunID: runID, Status: "accepted"}, nil
	default:
		c.cancelRun()
		c.runCtx, c.cancelRun = nil, nil
		return runReceipt{}, errCommandQueueFull
	}
}

// rememberRun records a validated legacy start without exposing model references.
func (c *runController) rememberRun(runID string, document []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = &runRecord{
		RunID:    runID,
		Status:   "running",
		Scenario: append([]byte(nil), document...),
	}
}

// setRunStatus updates lifecycle metadata; the scenario remains the initial document.
func (c *runController) setRunStatus(runID, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil && c.current.RunID == runID {
		c.current.Status = status
	}
}

// currentRun returns a detached record for HTTP readers, never the live simulation.
func (c *runController) currentRun() *runRecord {
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
func (c *runController) forgetRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = nil
}

func (c *runController) contextForRun() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runCtx
}

func (c *runController) finishRun() {
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
func (c *runController) acknowledgeStop(publish func() error) error {
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
