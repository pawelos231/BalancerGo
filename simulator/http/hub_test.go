package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"load-balancer/internal/contract"
)

func hubSnapshot(runID, status string) contract.SimulationSnapshot {
	return contract.SimulationSnapshot{
		Type:       "simulationSnapshot",
		Simulation: contract.SimulationMetadata{RunID: runID, Status: status},
	}
}

func registerHubClient(t *testing.T, h *Hub) *wsClient {
	t.Helper()
	c := newWSClient(nil)
	if err := h.registerClient(c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.unregisterClient(c) })
	return c
}

func expectHubFrame(t *testing.T, c *wsClient, want any) {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.takeFrame()
	if !ok || !bytes.Equal(got, encoded) {
		t.Fatalf("next frame = %s (present %v), want %s", got, ok, encoded)
	}
}

func expectHubQueueEmpty(t *testing.T, c *wsClient) {
	t.Helper()
	if frame, ok := c.takeFrame(); ok {
		t.Fatalf("unexpected pending frame: %s", frame)
	}
}

func TestHubEndBarrierOrdersCriticalFramesBeforeNewRun(t *testing.T) {
	h := NewHub(nil)
	c := registerHubClient(t, h)
	initial := hubSnapshot("run-A", "running")
	if err := h.BroadcastState(initial); err != nil {
		t.Fatal(err)
	}
	// A frame already taken by the writer may finish before the end barrier.
	expectHubFrame(t, c, initial)
	priorReply := contract.CommandResultFrame{Type: "commandResult", CommandID: "prior"}
	c.replyCommand(priorReply)
	if err := h.BroadcastState(hubSnapshot("run-A", "finished")); err != nil {
		t.Fatal(err)
	}
	ended := contract.SimulationEndedFrame{
		Type: "simulationEnded", RunID: "run-A", Reason: contract.SimulationFinished,
	}
	if err := h.BroadcastEnded(ended); err != nil {
		t.Fatal(err)
	}
	stopReply := contract.CommandResultFrame{Type: "commandResult", CommandID: "stop-A"}
	c.replyCommand(stopReply)
	if err := h.BroadcastState(hubSnapshot("run-B", "running")); err != nil {
		t.Fatal(err)
	}
	latest := hubSnapshot("run-B", "paused")
	if err := h.BroadcastState(latest); err != nil {
		t.Fatal(err)
	}
	expectHubFrame(t, c, priorReply)
	expectHubFrame(t, c, ended)
	expectHubFrame(t, c, stopReply)
	expectHubFrame(t, c, latest)
	expectHubQueueEmpty(t, c)
}

func TestHubPrivateReplyFIFOLeavesIdlePending(t *testing.T) {
	h := NewHub(nil)
	owner := registerHubClient(t, h)
	other := registerHubClient(t, h)
	idle := hubSnapshot("", "idle")
	if err := h.BroadcastState(idle); err != nil {
		t.Fatal(err)
	}
	first := contract.CommandResultFrame{Type: "commandResult", CommandID: "first"}
	second := contract.CommandResultFrame{Type: "commandResult", CommandID: "second"}
	owner.replyCommand(first)
	owner.replyCommand(second)
	expectHubFrame(t, owner, first)
	expectHubFrame(t, owner, second)
	expectHubFrame(t, owner, idle)
	expectHubQueueEmpty(t, owner)
	expectHubFrame(t, other, idle)
	expectHubQueueEmpty(t, other)
}

func TestHubCriticalOverflowStopsSlowClient(t *testing.T) {
	for _, kind := range []string{"end", "private reply"} {
		t.Run(kind, func(t *testing.T) {
			h := NewHub(nil)
			slow := registerHubClient(t, h)
			other := registerHubClient(t, h)
			for i := 0; i < criticalQueueCapacity; i++ {
				slow.replyCommand(contract.CommandResultFrame{Type: "commandResult"})
			}
			select {
			case <-slow.done:
				t.Fatal("client stopped before the critical queue filled")
			default:
			}
			ended := contract.SimulationEndedFrame{
				Type: "simulationEnded", RunID: "run-A", Reason: contract.SimulationStopped,
			}
			if kind == "end" {
				if err := h.BroadcastEnded(ended); err != nil {
					t.Fatal(err)
				}
				expectHubFrame(t, other, ended)
			} else {
				slow.replyCommand(contract.CommandResultFrame{Type: "commandResult"})
				expectHubQueueEmpty(t, other)
			}
			select {
			case <-slow.done:
			default:
				t.Fatal("critical queue overflow did not stop the client")
			}
			select {
			case <-other.done:
				t.Fatal("overflow stopped another client")
			default:
			}
			if err := h.BroadcastState(hubSnapshot("run-B", "running")); err != nil {
				t.Fatal(err)
			}
			expectHubQueueEmpty(t, slow)
			expectHubFrame(t, other, hubSnapshot("run-B", "running"))
		})
	}
}

func TestHubRegistrationReplaysCurrentCache(t *testing.T) {
	h := NewHub(nil)
	initial := hubSnapshot("run-A", "running")
	if err := h.BroadcastState(initial); err != nil {
		t.Fatal(err)
	}
	first := registerHubClient(t, h)
	expectHubFrame(t, first, initial)
	finalSnapshot := hubSnapshot("run-A", "finished")
	ended := contract.SimulationEndedFrame{
		Type: "simulationEnded", RunID: "run-A", Reason: contract.SimulationFinished,
		FinalSnapshot:      &finalSnapshot,
		TopologyDefinition: json.RawMessage(`{"runId":"run-A"}`),
	}
	if err := h.BroadcastEnded(ended); err != nil {
		t.Fatal(err)
	}
	reconnected := registerHubClient(t, h)
	if len(reconnected.critical) != 1 || reconnected.latest != nil {
		t.Fatal("reconnected client did not receive the cached end as a critical frame")
	}
	next := hubSnapshot("run-B", "running")
	if err := h.BroadcastState(next); err != nil {
		t.Fatal(err)
	}
	expectHubFrame(t, reconnected, ended)
	expectHubFrame(t, reconnected, next)
	expectHubQueueEmpty(t, reconnected)
	fresh := registerHubClient(t, h)
	expectHubFrame(t, fresh, next)
	expectHubQueueEmpty(t, fresh)
}

func TestHubIdleReplacesRetainedEnd(t *testing.T) {
	h := NewHub(nil)
	if err := h.BroadcastEnded(contract.SimulationEndedFrame{
		Type: "simulationEnded", RunID: "run-A", Reason: contract.SimulationStopped,
	}); err != nil {
		t.Fatal(err)
	}
	idle := hubSnapshot("", "idle")
	if err := h.BroadcastState(idle); err != nil {
		t.Fatal(err)
	}
	c := registerHubClient(t, h)
	expectHubFrame(t, c, idle)
	expectHubQueueEmpty(t, c)
}

func TestHubShutdownStopsClientsAndRejectsPublications(t *testing.T) {
	h := NewHub(nil)
	c := registerHubClient(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Run(ctx)
	h.Run(context.Background())
	select {
	case <-c.done:
	default:
		t.Fatal("shutdown left a client running")
	}
	if err := h.BroadcastState(hubSnapshot("", "idle")); !errors.Is(err, errHubClosed) {
		t.Fatalf("snapshot publication after shutdown = %v", err)
	}
	if err := h.BroadcastEnded(contract.SimulationEndedFrame{}); !errors.Is(err, errHubClosed) {
		t.Fatalf("end publication after shutdown = %v", err)
	}
	if err := h.registerClient(newWSClient(nil)); !errors.Is(err, errHubClosed) {
		t.Fatalf("registration after shutdown = %v", err)
	}
}
