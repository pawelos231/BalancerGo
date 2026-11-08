package main

import (
	"encoding/json"
	"errors"
	"strings"

	"load-balancer/internal/contract"
)

// stopObservation is detached before the model and HTTP topology are cleared.
type stopObservation struct {
	snapshot *contract.SimulationSnapshot
	topology json.RawMessage
}

// stopRequest keeps the target and reply together while the owner finishes work.
// A Stop for run A must never clear a newly accepted run B.
type stopRequest struct {
	runID     string
	commandID string
	reply     func(contract.CommandResultFrame)
}

// stopTargetLocked includes pending starts and retained finished/failed runs.
func (c *runController) stopTargetLocked() string {
	if c.reservedRunID != "" {
		return c.reservedRunID
	}
	if c.current != nil {
		return c.current.RunID
	}
	return ""
}

// enqueueStop checks the target before cancelling pacing; the caller holds mu.
func (c *runController) enqueueStop(command runtimeCommand) error {
	if strings.TrimSpace(command.runID) == "" || strings.TrimSpace(command.commandID) == "" {
		return errors.New("Stop requires runId and commandId")
	}
	if command.runID != c.stopTargetLocked() {
		return errors.New("Stop target does not match the current simulation")
	}
	if c.pendingStop != nil {
		return errors.New("a Stop command is already pending")
	}

	request := &stopRequest{
		runID:     command.runID,
		commandID: command.commandID,
		reply:     command.reply,
	}
	select {
	case c.stops <- request:
		c.pendingStop = request
		if c.cancelRun != nil {
			c.cancelRun()
		}
		return nil
	default:
		return errCommandQueueFull
	}
}

// applyStop rechecks the target on the owner, then clears it before replying.
// Admission stays blocked while clear publishes idle, so another Start cannot slip in.
func (c *runController) applyStop(request *stopRequest, clear func() (stopObservation, error)) error {
	c.mu.Lock()
	if c.pendingStop != request || request.runID != c.stopTargetLocked() {
		if c.pendingStop == request {
			c.pendingStop = nil
		}
		c.mu.Unlock()
		request.respond(errors.New("Stop target is no longer the current simulation"), stopObservation{})
		return nil
	}

	c.finishRunLocked()
	c.current = nil
	c.reservedRunID = ""
	observation, err := clear()
	c.mu.Unlock()

	// Keep Start blocked until the reply is queued, without calling I/O under mu.
	request.respond(err, observation)
	c.mu.Lock()
	c.pendingStop = nil
	c.mu.Unlock()
	return err
}

func (request *stopRequest) respond(err error, observation stopObservation) {
	if request.reply == nil {
		return
	}
	result := contract.CommandResultFrame{
		Type:        "commandResult",
		CommandType: contract.StopSimulation,
		RunID:       request.runID,
		CommandID:   request.commandID,
		Status:      contract.CommandApplied,
	}
	if err != nil {
		result.Status = contract.CommandRejected
		result.Error = err.Error()
	} else {
		result.FinalSnapshot = observation.snapshot
		result.TopologyDefinition = observation.topology
	}
	request.reply(result)
}
