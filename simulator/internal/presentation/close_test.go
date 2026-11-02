package presentation

import (
	"encoding/json"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/scenario"
)

// Payload success stays independent from the current close state and its counters.
func TestCloseProjectionPreservesStateAndCumulativeRetries(t *testing.T) {
	native := model.CloseState{
		State: model.TCPTimeWait, FINRetransmissions: 2, Elapsed: 75 * time.Millisecond,
	}
	state := model.ClientState{
		Transfer: model.TransferState{Status: model.TransferSucceeded}, Connection: native,
	}
	metrics := metricValues(clientMetrics(simulation.ClientObservation{State: state}))
	if metrics["finRetransmissions"] != 2 || metrics["closeElapsedNanoseconds"] != float64(native.Elapsed) {
		t.Fatalf("closing metrics = %+v", metrics)
	}
	reported := clientState(state)
	if reported.Transfer.Status != contract.TransferSucceeded || reported.Connection.State != "timeWait" ||
		reported.Connection.Outcome != "" || reported.Connection.FINRetransmissions != 2 ||
		reported.Connection.Elapsed != native.Elapsed {
		t.Fatalf("payload/close projection = %+v", reported)
	}
	native.State, native.Outcome = model.TCPClosed, model.CloseGraceful
	if terminal := closeState(native); terminal.State != "closed" || terminal.Outcome != "graceful" {
		t.Fatalf("terminal close projection = %+v", terminal)
	}
}

// Effective shutdown limits and every created connection survive route cleanup.
func TestResultRetainsConnectionAndCloseLimits(t *testing.T) {
	definition := projectionScenario()
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 64}
	msl, wait, retries := 2*time.Millisecond, 3*time.Second, 0
	definition.TCP.Close = scenario.CloseConfig{MSL: &msl, FinWaitTimeout: &wait, MaxFINRetries: &retries}
	world := projectionWorld(t, definition)
	advanceProjection(t, world, world.EndTime())
	if err := world.RemoveBackend("one"); err != nil {
		t.Fatal(err)
	}
	result := BuildResult(world, 0)
	if result.EffectiveClose.MSL != msl || result.EffectiveClose.FinWaitTimeout != wait || result.EffectiveClose.MaxFINRetries != 0 {
		t.Fatalf("effective close config = %+v", result.EffectiveClose)
	}
	if len(result.ClientConnection) != 1 || result.ClientConnection[0].ClientID != "client/clients/0" {
		t.Fatalf("retained connection = %+v", result.ClientConnection)
	}
	var native model.CloseState
	world.VisitClients(func(client simulation.ClientObservation) { native = client.State.Connection })
	if result.ClientConnection[0].State != closeState(native) {
		t.Fatal("report changed retained endpoint state")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		EffectiveClose   contract.CloseConfig        `json:"effectiveClose"`
		ClientConnection []contract.ClientConnection `json:"clientConnection"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EffectiveClose != result.EffectiveClose || len(decoded.ClientConnection) != 1 {
		t.Fatal("close report fields were absent from JSON")
	}
}

// Changing connection state uses a full live frame without topology revisions.
func TestCloseStateUsesExistingTelemetryFrames(t *testing.T) {
	definition := projectionScenario()
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 64, PacketGap: time.Millisecond}
	msl := 20 * time.Millisecond
	definition.TCP.Close.MSL = &msl
	world := projectionWorld(t, definition)
	advanceProjection(t, world, time.Millisecond)
	projector := NewProjector("close")
	first := project(t, projector, world)
	advanceProjection(t, world, 10*time.Millisecond)
	second := project(t, projector, world)
	if second.Document != nil || second.Topology.Revision != first.Topology.Revision {
		t.Fatal("closing changed topology identity")
	}
	var native model.CloseState
	world.VisitClients(func(client simulation.ClientObservation) { native = client.State.Connection })
	var found bool
	for _, node := range second.Topology.Nodes {
		if node.ID != "client/clients/0" {
			continue
		}
		found = true
		if node.TCPState != string(native.State) || node.CloseOutcome != string(native.Outcome) ||
			node.Metrics["finRetransmissions"] != float64(native.FINRetransmissions) ||
			node.Metrics["closeElapsedNanoseconds"] != float64(native.Elapsed) {
			t.Fatalf("live close frame = %+v, native = %+v", node, native)
		}
	}
	if !found {
		t.Fatal("client disappeared from closing topology")
	}
}
