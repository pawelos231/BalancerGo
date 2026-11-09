package server_test

import (
	"testing"
	"time"

	"load-balancer/host"
	"load-balancer/model"
)

func TestPhysicalAvailabilityPreservesDrainAndHealthObservation(t *testing.T) {
	for _, draining := range []bool{false, true} {
		h := newByteServerHarness(t)
		t.Cleanup(h.server.Shutdown)
		h.runUntil(5 * time.Millisecond)
		if draining {
			h.server.StartDrain()
		}
		h.server.UpdateHealth(!draining, 17)
		drainStarted := h.server.DrainStartedAt

		h.server.MarkFailed()
		failed := h.server.GetState()
		if failed.Active || failed.Draining != draining || failed.HealthTCP != !draining ||
			failed.LatencyP95Milli != 17 || h.server.DrainStartedAt != drainStarted {
			t.Fatalf("physical failure changed independent state: %+v", failed)
		}
		h.server.MarkHealthy()
		recovered := h.server.GetState()
		if !recovered.Active || recovered.Draining != draining || recovered.HealthTCP != !draining ||
			recovered.LatencyP95Milli != 17 || h.server.DrainStartedAt != drainStarted {
			t.Fatalf("physical recovery changed independent state: %+v", recovered)
		}
	}
}

func TestDrainedUnhealthyBackendProcessesPreviouslySelectedSYN(t *testing.T) {
	h := newByteServerHarness(t)
	t.Cleanup(h.server.Shutdown)
	h.server.SetHostConfig(host.Config{
		Cores: 1, PacketProcessingTime: 10 * time.Microsecond, QueueCapacity: 2,
	})
	h.receive(model.FlagSYN, 0, 0, 0)
	h.server.StartDrain()
	h.server.UpdateHealth(false, 17)
	if h.server.CanAcceptNewConnection() {
		t.Fatal("draining backend remained eligible for new selection")
	}
	h.runUntil(10 * time.Microsecond)
	h.receive(model.FlagSYN, 0, 0, 0)
	h.runUntil(20 * time.Microsecond)
	if len(h.replies) != 2 || h.server.HalfOpenConnections != 1 {
		t.Fatalf("selected SYN or its retry was blocked by drain: replies=%+v", h.replies)
	}
	h.receive(model.FlagACK, 1, 1, 4)
	probeReplies := 0
	h.server.HandleProbe(func() { probeReplies++ })
	h.runUntil(30 * time.Microsecond)
	if state := h.server.GetState(); state.ActiveConnections != 1 ||
		state.ReceivedPayloadBytes != 4 || probeReplies != 1 {
		t.Fatalf("drain or observed health blocked local work: state=%+v probes=%d", state, probeReplies)
	}
}

func TestPhysicalFailureCancelsLocalConnectionTimersAndProbes(t *testing.T) {
	for _, phase := range []string{"half-open", "idle", "delayed ACK", "FIN retry", "FIN wait", "time wait"} {
		t.Run(phase, func(t *testing.T) {
			h := newByteServerHarness(t)
			t.Cleanup(h.server.Shutdown)
			h.server.SetReceiveBufferBytes(32)
			h.server.SetIdleTimeouts(100*time.Millisecond, 100*time.Millisecond)
			h.server.SetDelayedACKConfig(model.DelayedACKConfig{
				Enabled: true, SegmentsPerACK: 2, MaxDelay: 40 * time.Millisecond,
			})
			h.server.SetCloseConfig(model.CloseConfig{
				MSL: 10 * time.Millisecond, FinWaitTimeout: 50 * time.Millisecond, MaxFINRetries: 2,
			})
			h.receive(model.FlagSYN, 0, 0, 0)
			if phase != "half-open" {
				h.receive(model.FlagACK, 1, 1, 0)
			}
			switch phase {
			case "delayed ACK":
				h.receive(model.FlagACK, 1, 1, 2)
				h.receive(model.FlagACK, 3, 1, 2)
				h.receive(model.FlagACK, 5, 1, 2)
			case "FIN retry", "FIN wait", "time wait":
				h.server.CloseConnection(h.flow)
				if phase == "FIN wait" {
					h.receive(model.FlagACK, 1, 2, 0)
				} else if phase == "time wait" {
					h.receive(model.FlagFIN|model.FlagACK, 1, 2, 0)
				}
			}
			probeReplies := 0
			h.server.HandleProbe(func() { probeReplies++ })
			before := h.server.GetState()
			repliesBefore := len(h.replies)
			if h.engine.Pending() < 2 {
				t.Fatal("fixture did not schedule a local connection timer and probe")
			}

			h.server.SetPhysicalAvailable(false)
			if h.engine.Pending() != 0 || h.server.ActiveConnections != 0 || h.server.HalfOpenConnections != 0 ||
				h.server.GetConnectionState(h.flow).State != model.TCPClosed {
				t.Fatalf("physical failure left local connection work: pending=%d", h.engine.Pending())
			}
			h.receive(model.FlagSYN, 0, 0, 0)
			h.server.HandleProbe(func() { probeReplies++ })
			h.runUntil(200 * time.Millisecond)
			if len(h.replies) != repliesBefore || probeReplies != 0 ||
				h.server.HostStats().OfferedPackets != before.Host.OfferedPackets {
				t.Fatal("unavailable backend accepted packets, CPU work or probes")
			}

			h.server.SetPhysicalAvailable(true)
			after := h.server.GetState()
			if after.ReceivedPayloadBytes != before.ReceivedPayloadBytes || after.ACK != before.ACK ||
				after.ECN != before.ECN || after.NumberOfHandledPackets != before.NumberOfHandledPackets ||
				after.Host.ProcessedPackets != before.Host.ProcessedPackets {
				t.Fatalf("recovery erased cumulative history: before=%+v after=%+v", before, after)
			}
			// Old payload cannot recreate the lost connection; a new SYN can reuse its tuple.
			h.receive(model.FlagACK, 7, 1, 4)
			if h.server.GetState().ReceivedPayloadBytes != before.ReceivedPayloadBytes {
				t.Fatal("recovery revived the old byte stream")
			}
			h.receive(model.FlagSYN, 100, 0, 0)
			h.assertReplyWindow(model.FlagSYN|model.FlagACK, 0, 101, 32)
			h.receive(model.FlagACK, 101, 1, 2)
			h.runUntil(239 * time.Millisecond)
			if len(h.replies) != repliesBefore+1 {
				t.Fatal("recovery lost delayed ACK configuration")
			}
			h.runUntil(240 * time.Millisecond)
			h.assertReplyWindow(model.FlagACK, 1, 103, 32)
			if h.server.GetACKState().TimerTriggeredACKCount != before.ACK.TimerTriggeredACKCount+1 {
				t.Fatal("recovery lost cumulative ACK counters")
			}
		})
	}
}

func TestPhysicalFailureLeavesAlreadyEmittedRepliesInFlight(t *testing.T) {
	h := newByteServerHarness(t)
	t.Cleanup(h.server.Shutdown)
	h.server.SetSender(func(packet model.Packet) {
		h.engine.ScheduleAfter(10*time.Millisecond, func() {
			h.replies = append(h.replies, packet)
		})
	})
	h.receive(model.FlagSYN, 0, 0, 0)
	h.server.SetPhysicalAvailable(false)
	h.runUntil(10 * time.Millisecond)
	if len(h.replies) != 1 || h.replies[0].Flag != model.FlagSYN|model.FlagACK {
		t.Fatalf("crash canceled an already emitted reply: %+v", h.replies)
	}
}
