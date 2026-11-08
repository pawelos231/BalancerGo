package contract

import "encoding/json"

type CommandResultStatus string

const (
	CommandApplied  CommandResultStatus = "applied"
	CommandRejected CommandResultStatus = "rejected"
)

// CommandResultFrame answers a command on its submitting connection.
// An applied Stop confirms cleanup; its optional sample was captured beforehand.
type CommandResultFrame struct {
	Type        string              `json:"type"`
	CommandType CommandType         `json:"commandType"`
	RunID       string              `json:"runId"`
	CommandID   string              `json:"commandId"`
	Status      CommandResultStatus `json:"status"`
	Error       string              `json:"error,omitempty"`

	// Stop captures these before clearing, including when it clears a finished run.
	FinalSnapshot      *SimulationSnapshot `json:"finalSnapshot,omitempty"`
	TopologyDefinition json.RawMessage     `json:"topologyDefinition,omitempty"`
}
