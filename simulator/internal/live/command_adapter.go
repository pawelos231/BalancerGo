package live

import (
	"context"
	"errors"
	"fmt"
	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
)

// submitFrontendCommand translates a wire action without decoding its runtime
// payload early. The application still reports those errors at the DES boundary.
func submitFrontendCommand(
	control *runController,
	ctx context.Context,
	input contract.Envelope,
	reply func(contract.CommandResultFrame),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var kind commandKind
	switch input.Type {
	case contract.StartSimulation:
		kind = startCommand
	case contract.StopSimulation:
		kind = stopCommand
	case contract.SetAlgorithm:
		kind = algorithmCommand
	case contract.SpawnClient:
		kind = spawnCommand
	default:
		return fmt.Errorf("unsupported command: %s", input.Type)
	}
	return control.submit(ctx, runtimeCommand{
		kind:      kind,
		Payload:   input.Payload,
		runID:     input.RunID,
		commandID: input.CommandID,
		reply:     reply,
	})
}

// admissionError preserves the public HTTP status mapping for application errors.
func admissionError(err error) error {
	switch {
	case errors.Is(err, errSimulationBusy):
		return httpserver.ErrSimulationBusy
	case errors.Is(err, errCommandQueueFull):
		return httpserver.ErrCommandQueueFull
	case errors.Is(err, errServiceClosed):
		return httpserver.ErrServiceClosed
	default:
		return err
	}
}
