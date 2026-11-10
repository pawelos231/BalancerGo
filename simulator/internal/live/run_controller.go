package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"load-balancer/internal/contract"
	"load-balancer/internal/execution"
	"load-balancer/internal/simulation"
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

// runtimeCommand carries Start input or an addressed Stop to the run owner.
type runtimeCommand struct {
	kind      commandKind
	Payload   []byte
	runID     string
	commandID string
	reply     func(contract.CommandResultFrame)
}

type runReceipt struct{ RunID, Status string }
type runRecord struct {
	RunID, Status string
	Scenario      []byte
	Experiment    contract.ExperimentContext
}

var (
	errSimulationBusy   = errors.New("simulation is already running or a request is pending")
	errCommandQueueFull = errors.New("simulation command queue is full")
	errServiceClosed    = errors.New("simulation service is closed")
)

// startRequest carries a checked scenario or settings awaiting validation.
// Only the simulation owner creates the world from this request.
type startRequest struct {
	runID      string
	command    *runtimeCommand
	plan       *scenario.Compiled
	experiment contract.ExperimentContext
}

// runController coordinates I/O requests without accessing live models.
type runController struct {
	mu            sync.Mutex
	starts        chan startRequest
	stops         chan *stopRequest
	serviceCtx    context.Context
	current       *runRecord
	runCtx        context.Context
	cancelRun     context.CancelFunc
	reservedRunID string // Includes accepted starts whose model has not been created yet.
	pendingStop   *stopRequest
}

func newRunController(ctx context.Context) *runController {
	return &runController{
		starts:     make(chan startRequest, 1),
		stops:      make(chan *stopRequest, 1),
		serviceCtx: ctx,
	}
}

// submit admits Start and Stop; timeline changes belong in the scenario document.
// Stop cancels the run and signals the owner to clear its state.
func (c *runController) submit(ctx context.Context, command runtimeCommand) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.serviceCtx.Err() != nil {
		return errServiceClosed
	}

	switch command.kind {
	case stopCommand:
		return c.enqueueStop(command)
	case startCommand:
		_, err := c.enqueueStart(startRequest{command: &command}, nil)
		return err
	case algorithmCommand, spawnCommand:
		if c.cancelRun == nil || c.pendingStop != nil {
			return errors.New("start the simulation before sending a command")
		}
		return simulation.ErrTimelineManualMutation
	default:
		return fmt.Errorf("unsupported application command: %d", command.kind)
	}

}

// startTimeline admits an immutable plan and retains the same canonical source.
func (c *runController) startTimeline(
	ctx context.Context,
	document []byte,
	plan *scenario.Compiled,
) (runReceipt, error) {
	if plan == nil {
		return runReceipt{}, errors.New("compiled scenario is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return runReceipt{}, err
	}
	request := startRequest{
		plan: plan,
		experiment: execution.Context(document, contract.ExperimentExecution{
			Speed: plan.Fixed().Execution.Speed,
		}),
	}
	return c.enqueueStart(request, document)
}

// enqueueStart requires the control lock; all transports use this admission check.
func (c *runController) enqueueStart(request startRequest, document []byte) (runReceipt, error) {
	if c.serviceCtx.Err() != nil {
		return runReceipt{}, errServiceClosed
	}
	if c.cancelRun != nil || c.pendingStop != nil {
		return runReceipt{}, errSimulationBusy
	}
	runID, err := newRunID()
	if err != nil {
		return runReceipt{}, err
	}

	request.runID = runID
	c.runCtx, c.cancelRun = context.WithCancel(c.serviceCtx)
	select {
	case c.starts <- request:
		c.reservedRunID = runID
		if document != nil {
			c.current = &runRecord{
				RunID:      runID,
				Status:     "accepted",
				Scenario:   append([]byte(nil), document...),
				Experiment: request.experiment,
			}
		}
		return runReceipt{RunID: runID, Status: "accepted"}, nil
	default:
		c.cancelRun()
		c.runCtx, c.cancelRun = nil, nil
		return runReceipt{}, errCommandQueueFull
	}
}

// newRunID uses fresh randomness, so restarting the backend does not restart a counter.
func newRunID() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate simulation run ID: %w", err)
	}
	return "run-" + hex.EncodeToString(randomBytes[:]), nil
}

// rememberRun records a validated WebSocket start without exposing model references.
func (c *runController) rememberRun(
	runID string,
	document []byte,
	experiment contract.ExperimentContext,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = &runRecord{
		RunID:      runID,
		Status:     "running",
		Scenario:   append([]byte(nil), document...),
		Experiment: experiment,
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
	if modified := record.Experiment.Build.VCSModified; modified != nil {
		value := *modified
		record.Experiment.Build.VCSModified = &value
	}
	return &record
}

func (c *runController) contextForRun() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runCtx
}

func (c *runController) finishRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishRunLocked()
}

// finishNaturally chooses normal completion or an already admitted Stop under one lock.
// The terminal message is queued before another Start can reserve the slot.
func (c *runController) finishNaturally(runID, status string, publishEnd func() error) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingStop != nil {
		return false, nil
	}
	if c.reservedRunID != runID {
		return false, errors.New("completed run is no longer the reserved simulation")
	}
	if c.current != nil && c.current.RunID == runID {
		c.current.Status = status
	}
	if err := publishEnd(); err != nil {
		return false, err
	}
	c.finishRunLocked()
	return true, nil
}

// finishRunLocked releases the slot but retains the target of an accepted Stop.
func (c *runController) finishRunLocked() {
	if c.cancelRun != nil {
		c.cancelRun()
	}
	c.runCtx, c.cancelRun = nil, nil
	if c.pendingStop == nil {
		c.reservedRunID = ""
	}
	// Submission uses the same lock, so old commands cannot enter the next run.
	for len(c.starts) > 0 {
		<-c.starts
	}
}
