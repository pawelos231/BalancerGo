package httpserver

import "encoding/json"

//go:generate go run generate_commands.go

// CommandType identifies an action sent by the frontend.
type CommandType string

const (
	StartSimulation CommandType = "startSimulation"
	StopSimulation  CommandType = "stopSimulation"
	SetAlgorithm    CommandType = "setAlgorithm"
	SpawnClient     CommandType = "spawnClient"
)

// Envelope carries a command; the simulation owner decodes its payload.
type Envelope struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type SetAlgorithmMsg struct {
	Name string `json:"name"`
}

type SpawnClientMsg struct {
	Count int `json:"count"`
}
