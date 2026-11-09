package contract_test

import (
	"encoding/json"
	"testing"
	"time"

	"load-balancer/internal/contract"
)

// These fixtures freeze existing wire choices that differ from domain values:
// PascalCase report fields, flattened counters, nanoseconds and virtual seconds.
func TestLegacyLinkReportWireFormat(t *testing.T) {
	report := contract.LinkResult{
		Name: "wire",
		Config: contract.LinkConfig{
			BandwidthBitsPerSecond: 1000,
			PacketTravelTime:       time.Millisecond + 7*time.Nanosecond,
			QueueCapacity:          4,
			LossRate:               0.1,
		},
		LinkStats: contract.LinkStats{OfferedPackets: 1, TransmittedBytes: 42, Transmitting: true},
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
	transfer := contract.TransferState{
		Status:                   contract.TransferSucceeded,
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
	snapshot := contract.SimulationSnapshot{
		Type:            "simulationSnapshot",
		SnapshotVersion: contract.CurrentSnapshotVersion,
		Mode:            contract.LBModeNAT,
		Algorithm:       "RoundRobin",
		Servers:         []contract.TelemetryServer{},
		Simulation: contract.SimulationMetadata{
			Status:    "idle",
			Algorithm: "roundRobin",
		},
		Topology: contract.TelemetryTopology{
			Revision: "0",
			Nodes:    []contract.TelemetryNode{},
			Links:    []contract.TelemetryLink{},
		},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"simulationSnapshot","snapshotVersion":1,"mode":"NAT","algorithm":"RoundRobin","servers":[],"simulation":{"status":"idle","algorithm":"roundRobin","virtualTimeSeconds":0},"topology":{"revision":"0","nodes":[],"links":[]}}`
	if string(encoded) != want {
		t.Fatalf("idle snapshot changed:\n%s\nwant:\n%s", encoded, want)
	}
}

func TestConnectionCloseWireFormat(t *testing.T) {
	state := contract.CloseState{
		State: "timeWait", FINRetransmissions: 2, Elapsed: 75 * time.Millisecond,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"state":"timeWait","finRetransmissions":2,"elapsedNanoseconds":75000000}`; string(encoded) != want {
		t.Fatalf("close state wire = %s, want %s", encoded, want)
	}
	state.State, state.Outcome = "closed", "graceful"
	encoded, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"state":"closed","outcome":"graceful","finRetransmissions":2,"elapsedNanoseconds":75000000}`; string(encoded) != want {
		t.Fatalf("terminal close state wire = %s, want %s", encoded, want)
	}
}
