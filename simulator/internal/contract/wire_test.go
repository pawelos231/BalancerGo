package contract

import (
	"encoding/json"
	"testing"
	"time"
)

// These fixtures freeze existing wire choices that differ from domain values:
// PascalCase report fields, flattened counters, nanoseconds and virtual seconds.
func TestLegacyLinkReportWireFormat(t *testing.T) {
	report := LinkResult{
		Name: "wire",
		Config: LinkConfig{
			BandwidthBitsPerSecond: 1000,
			PacketTravelTime:       time.Millisecond + 7*time.Nanosecond,
			QueueCapacity:          4,
			LossRate:               0.1,
		},
		LinkStats: LinkStats{OfferedPackets: 1, TransmittedBytes: 42, Transmitting: true},
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Name":"wire","Config":{"BandwidthBitsPerSecond":1000,"PacketTravelTime":1000007,"QueueCapacity":4,"LossRate":0.1},"OfferedPackets":1,"OfferedBytes":0,"AcceptedPackets":0,"AcceptedBytes":0,"QueueDroppedPackets":0,"QueueDroppedBytes":0,"TransmittedBytes":42,"LostPackets":0,"LostBytes":0,"DeliveredPackets":0,"DeliveredBytes":0,"QueuedPackets":0,"Transmitting":true,"TravellingPackets":0}`
	if string(encoded) != want {
		t.Fatalf("legacy link report changed:\n%s\nwant:\n%s", encoded, want)
	}
}

func TestTransferSecondsAndOptionalFailureWireFormat(t *testing.T) {
	transfer := TransferState{
		Status:                   TransferSucceeded,
		ElapsedSeconds:           1.25,
		HandshakeCompleted:       true,
		HandshakeDurationSeconds: 0.25,
		PayloadDurationSeconds:   1,
		SynRetransmissions:       2,
		DataRetransmissions:      3,
	}
	encoded, err := json.Marshal(transfer)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"succeeded","elapsedSeconds":1.25,"handshakeCompleted":true,"handshakeDurationSeconds":0.25,"payloadDurationSeconds":1,"synRetransmissions":2,"dataRetransmissions":3}`
	if string(encoded) != want {
		t.Fatalf("public transfer changed:\n%s\nwant:\n%s", encoded, want)
	}
}

func TestIdleSnapshotWireFormat(t *testing.T) {
	snapshot := SimulationSnapshot{
		Type:      "simulationSnapshot",
		Version:   1,
		Mode:      LBModeNAT,
		Algorithm: "RoundRobin",
		Servers:   []TelemetryServer{},
		Simulation: SimulationMetadata{
			Status:    "idle",
			Algorithm: "roundRobin",
		},
		Topology: TelemetryTopology{
			Revision: "0",
			Nodes:    []TelemetryNode{},
			Links:    []TelemetryLink{},
		},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"simulationSnapshot","version":1,"mode":"NAT","algorithm":"RoundRobin","servers":[],"simulation":{"status":"idle","algorithm":"roundRobin","virtualTimeSeconds":0},"topology":{"revision":"0","nodes":[],"links":[]}}`
	if string(encoded) != want {
		t.Fatalf("idle snapshot changed:\n%s\nwant:\n%s", encoded, want)
	}
}
