package main

import (
	"errors"
	"math"
	"strconv"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
)

// runPublication owns one run's browser messages and retained final measurement.
// It numbers samples before queuing and embeds definitions in the end message.
type runPublication struct {
	hub        *httpserver.Hub
	topology   *httpserver.Publication
	projector  *presentation.Projector
	runID      string
	experiment *contract.ExperimentContext
	started    time.Time
	sequence   uint64
	ended      *contract.SimulationEndedFrame
}

func (publication *runPublication) snapshot(
	world *simulation.World,
	state presentation.RunState,
) (contract.SimulationSnapshot, error) {
	state.RunID = publication.runID
	frame, definition, err := presentation.BuildSnapshot(world, publication.projector, state)
	if err != nil {
		return frame, err
	}
	frame.Simulation.Experiment = publication.experiment
	if world == nil {
		publication.topology.ClearTopology()
		return frame, nil
	}
	if definition != nil {
		if err := publication.topology.PublishTopology(definition); err != nil {
			return frame, err
		}
	}
	if state.Status == "running" {
		counters := frame.Simulation.Performance
		frame.Simulation.Performance = presentation.MeasurePerformance(
			time.Since(publication.started),
			counters.EventsExecuted,
			counters.ForwardedPackets,
		)
	}
	if publication.sequence == math.MaxUint64 {
		return frame, errors.New("simulation publication sequence exhausted")
	}
	publication.sequence++
	frame.Simulation.PublicationSequence = strconv.FormatUint(publication.sequence, 10)
	return frame, nil
}

func (publication *runPublication) publish(world *simulation.World, state presentation.RunState) error {
	frame, err := publication.snapshot(world, state)
	if err != nil {
		return err
	}
	return publication.hub.BroadcastState(frame)
}

// end queues one terminal outcome. Clearing an already finished run reuses it.
func (publication *runPublication) end(
	world *simulation.World,
	state presentation.RunState,
	reason contract.SimulationEndReason,
	commandID string,
) (stopObservation, error) {
	if publication.ended == nil {
		frame, err := publication.snapshot(world, state)
		if err != nil {
			return stopObservation{}, err
		}
		ended := contract.SimulationEndedFrame{
			Type:               "simulationEnded",
			RunID:              publication.runID,
			Reason:             reason,
			CommandID:          commandID,
			FinalSnapshot:      &frame,
			TopologyDefinition: publication.topology.CurrentTopology(),
		}
		if err := publication.hub.BroadcastEnded(ended); err != nil {
			return stopObservation{}, err
		}
		publication.ended = &ended
	}
	return stopObservation{
		snapshot: publication.ended.FinalSnapshot,
		topology: publication.ended.TopologyDefinition,
	}, nil
}
