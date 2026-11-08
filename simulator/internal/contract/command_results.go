package contract

type CommandResultStatus string

const (
	CommandApplied  CommandResultStatus = "applied"
	CommandRejected CommandResultStatus = "rejected"
)

// CommandResultFrame answers one Stop request on its submitting connection.
// Applied means the owner has cleared that run; idle alone does not confirm it.
type CommandResultFrame struct {
	Type        string              `json:"type"`
	CommandType CommandType         `json:"commandType"`
	RunID       string              `json:"runId"`
	CommandID   string              `json:"commandId"`
	Status      CommandResultStatus `json:"status"`
	Error       string              `json:"error,omitempty"`
}
