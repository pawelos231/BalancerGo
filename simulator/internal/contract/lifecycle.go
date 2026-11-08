package contract

import "encoding/json"

type SimulationEndReason string

const (
	SimulationFinished SimulationEndReason = "finished"
	SimulationFailed   SimulationEndReason = "failed"
	SimulationStopped  SimulationEndReason = "stopped"
)

// SimulationEndedFrame keeps the last measurement and its topology together.
// A Stop before model creation has no measurement, so both fields are null.
type SimulationEndedFrame struct {
	Type               string              `json:"type"`
	RunID              string              `json:"runId"`
	Reason             SimulationEndReason `json:"reason"`
	CommandID          string              `json:"commandId,omitempty"`
	FinalSnapshot      *SimulationSnapshot `json:"finalSnapshot"`
	TopologyDefinition json.RawMessage     `json:"topologyDefinition"`
}
