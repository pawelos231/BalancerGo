package server_test

import (
	"testing"
	"time"

	"load-balancer/host"
	"load-balancer/model"
)

// Five simultaneous SYNs fit two CPU cores and two waiting slots; one is dropped.
// TCP may create only two half-open connections at 10 us and four at 20 us.
func TestBackendCPUAdmissionPrecedesTCPHandshake(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetHostConfig(host.Config{
		Cores:                2,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        2,
	})
	t.Cleanup(h.server.Shutdown)
	for client := 0; client < 5; client++ {
		h.flow.SrcPort = uint16(1000 + client)
		h.receive(model.FlagSYN, 0, 0, 0)
	}
	if h.server.HalfOpenConnections != 0 || len(h.replies) != 0 {
		t.Fatal("SYN changed TCP state before CPU service finished")
	}

	runBackendCPU(t, h, 10*time.Microsecond)
	if h.server.HalfOpenConnections != 2 || len(h.replies) != 2 {
		t.Fatalf("first pair: half-open=%d replies=%d", h.server.HalfOpenConnections, len(h.replies))
	}
	runBackendCPU(t, h, 20*time.Microsecond)
	stats := h.server.HostStats()
	if h.server.HalfOpenConnections != 4 || len(h.replies) != 4 ||
		stats.ProcessedPackets != 4 || stats.QueueDroppedPackets != 1 {
		t.Fatalf("second pair or overflow was wrong: half-open=%d stats=%+v",
			h.server.HalfOpenConnections, stats)
	}
}

// The SYN, final handshake ACK and data each cost 10 us of backend CPU.
// Data reaches TCP at 30 us, so its 40-us delayed-ACK timer expires at 70 us.
func TestBackendDelayedACKStartsAfterCPUService(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetHostConfig(host.Config{
		Cores:                1,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        2,
	})
	h.server.SetDelayedACKConfig(model.DelayedACKConfig{
		Enabled:        true,
		SegmentsPerACK: 2,
		MaxDelay:       40 * time.Microsecond,
	})
	t.Cleanup(h.server.Shutdown)

	h.receive(model.FlagSYN, 0, 0, 0)
	runBackendCPU(t, h, 10*time.Microsecond)
	h.receive(model.FlagACK, 1, 1, 0)
	runBackendCPU(t, h, 20*time.Microsecond)
	h.receive(model.FlagPSH|model.FlagACK, 1, 1, 4)

	runBackendCPU(t, h, 29*time.Microsecond)
	if h.server.GetState().ReceivedPayloadBytes != 0 {
		t.Fatal("queued data was accepted by TCP before its processing cost elapsed")
	}
	runBackendCPU(t, h, 30*time.Microsecond)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 4 {
		t.Fatalf("processed payload=%d, want four bytes", got)
	}
	runBackendCPU(t, h, 69*time.Microsecond)
	if len(h.replies) != 1 {
		t.Fatal("delayed ACK counted CPU queueing time as receiver waiting time")
	}
	runBackendCPU(t, h, 70*time.Microsecond)
	if len(h.replies) != 2 || h.replies[1].Ack != 5 || h.server.GetACKState().TimerTriggeredACKCount != 1 {
		t.Fatalf("delayed ACK did not confirm the processed data: replies=%+v", h.replies)
	}
}

// Failure discards local CPU work. Recovery accepts fresh traffic but never revives it.
// A delivery predicate is checked again after CPU wait, before touching TCP state.
func TestBackendFailureAndShutdownDiscardPendingCPUWork(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "temporary failure"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			h := newByteServerHarness(t)
			h.server.SetHostConfig(host.Config{
				Cores:                1,
				PacketProcessingTime: 10 * time.Microsecond,
				QueueCapacity:        2,
			})
			t.Cleanup(h.server.Shutdown)
			h.receive(model.FlagSYN, 0, 0, 0)
			h.flow.SrcPort++
			h.receive(model.FlagSYN, 0, 0, 0)
			runBackendCPU(t, h, 5*time.Microsecond)

			if shutdown {
				h.server.Shutdown()
			} else {
				h.server.MarkFailed()
			}
			h.server.MarkHealthy()
			h.flow.SrcPort++
			freshFlow := h.flow
			h.receive(model.FlagSYN, 0, 0, 0)

			// This queued delivery belongs to an identity that is replaced before service ends.
			h.flow.SrcPort++
			valid := true
			h.server.HandlePacketIf(model.Packet{Key: h.flow, Flag: model.FlagSYN}, func() bool {
				return valid
			})
			valid = false
			runBackendCPU(t, h, 40*time.Microsecond)

			stats := h.server.HostStats()
			if stats.DiscardedPackets != 2 {
				t.Fatalf("old active/waiting packets were not discarded: %+v", stats)
			}
			if shutdown {
				if len(h.replies) != 0 || h.server.HalfOpenConnections != 0 || !stats.Stopped {
					t.Fatal("shutdown allowed TCP work to resume")
				}
				return
			}
			if len(h.replies) != 1 || h.replies[0].Key != freshFlow || h.server.HalfOpenConnections != 1 {
				t.Fatalf("recovery processed old or invalid deliveries: replies=%+v", h.replies)
			}
			if stats.OfferedPackets != 4 || stats.AcceptedPackets != 4 ||
				stats.ProcessedPackets != 2 || stats.BusyTime != 25*time.Microsecond {
				t.Fatalf("recovery lost CPU history: %+v", stats)
			}
		})
	}
}

func runBackendCPU(t *testing.T, h *byteServerHarness, until time.Duration) {
	t.Helper()
	if err := h.engine.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}
