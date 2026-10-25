// Package contract defines the existing public HTTP, WebSocket and CLI formats.
// Its values are detached from the simulation's mutable domain objects.
package contract

import "encoding/json"

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

// CommandErrorFrame reports admission failure on the submitting WebSocket.
type CommandErrorFrame struct {
	Type        string      `json:"type"`
	CommandType CommandType `json:"commandType"`
	Error       string      `json:"error"`
}

type SetAlgorithmMsg struct {
	Name string `json:"name"`
}

// SpawnClientMsg adds clients using an existing group's network and stream settings.
// GroupID may be omitted when the scenario contains exactly one group.
type SpawnClientMsg struct {
	Count   int    `json:"count"`
	GroupID string `json:"groupId,omitempty"`
}

// SimulationConfig is the legacy frontend experiment form. Process options such
// as tracing and headless mode are deliberately absent.
type SimulationConfig struct {
	Clients                 int     `json:"clients"`
	Servers                 int     `json:"servers"`
	Packets                 int     `json:"packets"`
	Bytes                   int     `json:"bytes"`
	DurationSeconds         float64 `json:"durationSeconds"`
	ClientGapMs             float64 `json:"clientGapMs"`
	PacketGapMs             float64 `json:"packetGapMs"`
	SnapshotIntervalMs      float64 `json:"snapshotIntervalMs"`
	BandwidthBitsPerSecond  int64   `json:"bandwidthBitsPerSecond"`
	ForwardingBitsPerSecond int64   `json:"forwardingBitsPerSecond"`
	PacketTravelTimeMs      float64 `json:"packetTravelTimeMs"`
	LinkQueue               int     `json:"linkQueue"`
	Loss                    float64 `json:"loss"`

	IngressBandwidthBitsPerSecond int64   `json:"ingressBandwidthBitsPerSecond"`
	IngressPacketTravelTimeMs     float64 `json:"ingressPacketTravelTimeMs"`
	IngressQueueCapacity          int     `json:"ingressQueueCapacity"`
	IngressLoss                   float64 `json:"ingressLoss"`

	Seed      int64   `json:"seed"`
	Algorithm string  `json:"algorithm"`
	Speed     float64 `json:"speed"`
	MaxEvents uint64  `json:"maxEvents"`
}

// RunReceipt identifies an accepted request before its model starts running.
type RunReceipt struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
}

// RunRecord keeps the canonical request alongside its latest lifecycle status.
// Scenario must contain an encoded scenario document with exact integer values.
type RunRecord struct {
	RunID    string          `json:"runId"`
	Status   string          `json:"status"`
	Scenario json.RawMessage `json:"scenario"`
}

// ScenarioSummary describes a validated workload without expanding its clients.
type ScenarioSummary struct {
	TotalClients     int `json:"totalClients"`
	BackendCount     int `json:"backendCount"`
	ClientGroupCount int `json:"clientGroupCount"`
}

type ScenarioValidationResponse struct {
	Scenario json.RawMessage `json:"scenario"`
	Summary  ScenarioSummary `json:"summary"`
	Resolved json.RawMessage `json:"resolved"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
