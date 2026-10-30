package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// With backoff, retries occur at 1, 3, 7, 15, 31, 63 and 123 s, capped to 60 s waits.
// Without it, both fixed and adaptive profiles retry the oldest segment every 1 s.
func TestTCPRTOBackoffIsIndependentOfEstimatorMode(t *testing.T) {
	for _, mode := range []model.RTOMode{model.RTOAdaptive, model.RTOFixed} {
		for _, backoff := range []bool{true, false} {
			name := string(mode) + "/backoff off"
			if backoff {
				name = string(mode) + "/backoff on"
			}
			t.Run(name, func(t *testing.T) {
				h := newAdaptiveTransferHarness(t)
				config := model.DefaultRTOConfig()
				config.Mode = mode
				config.ExponentialBackoff = backoff
				// A shared 1 s minimum isolates mode/backoff from the default RTT floor.
				config.MinimumTimeout = time.Second
				h.client.SetRTOConfig(config)
				h.client.SetIdleTimeout(0)

				h.client.SendCompleteStream(2048, 0)
				h.client.EstablishConnection()
				h.runUntil(0)
				h.receive(model.FlagSYN|model.FlagACK, 1)
				h.runUntil(0)

				deadline := time.Duration(0)
				wait := time.Second
				for retry := 1; retry <= 7; retry++ {
					deadline += wait
					h.runUntil(deadline - time.Nanosecond)
					if got := len(sentClientData(h.sent)); got != 2+retry-1 {
						t.Fatalf("retry %d fired before %s: %d payload transmissions", retry, deadline, got)
					}

					h.runUntil(deadline)
					data := sentClientData(h.sent)
					if len(data) != 2+retry || data[len(data)-1] != data[0] {
						t.Fatalf("retry %d did not resend only the oldest segment: %+v", retry, data)
					}
					if backoff {
						wait = min(2*wait, time.Minute)
					}
					assertRTOState(t, h, model.RTOState{
						BaseRTO:    time.Second,
						CurrentRTO: wait,
						Samples:    1,
					})
				}
			})
		}
	}
}

// A retry at 1 s invalidates RTT sampling; ACK=513 keeps the backed-off 2 s timer.
// After another retry, only new data sent after recovery may supply a fresh 250 ms RTT.
func TestTCPRTOKarnSkipsRetransmissionRecovery(t *testing.T) {
	h := newAdaptiveTransferHarness(t)
	// Keep the 1, 2 and 4 s waits easy to follow while testing Karn recovery.
	config := model.DefaultRTOConfig()
	config.MinimumTimeout = time.Second
	h.client.SetRTOConfig(config)
	h.client.SetIdleTimeout(0)
	h.client.SendCompleteStream(4096, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 2048)
	h.runUntil(time.Second)

	// ACK=513 confirms half of the retried head and permits 512 new bytes to leave.
	h.runUntil(1250 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 513, 2048)
	h.runUntil(h.engine.Now())
	assertRTOState(t, h, model.RTOState{
		BaseRTO:    time.Second,
		CurrentRTO: 2 * time.Second,
		Samples:    1,
	})

	// ACK progress restarts the timer using 2 s, rather than collapsing it to 1 s.
	h.runUntil(3250*time.Millisecond - time.Nanosecond)
	if got := len(sentClientData(h.sent)); got != 4 {
		t.Fatalf("ACK progress changed the backed-off retry deadline: %d transmissions", got)
	}
	h.runUntil(3250 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 5 || data[4].Seq != 513 || data[4].Size != 512 {
		t.Fatalf("second timeout did not retry the trimmed oldest range: %+v", data)
	}

	// ACK=2561 covers the retried suffix and untouched later data; none is a safe sample.
	h.runUntil(3500 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 2561, 2048)
	assertRTOState(t, h, model.RTOState{
		BaseRTO:    time.Second,
		CurrentRTO: 4 * time.Second,
		Samples:    1,
	})
	h.runUntil(h.engine.Now())

	// The final 1536 bytes are original sends after that recovery boundary.
	h.runUntil(3750 * time.Millisecond)
	h.receive(model.FlagACK, 4097)
	assertRTOState(t, h, model.RTOState{
		LatestRTT:    250 * time.Millisecond,
		SmoothedRTT:  31250 * time.Microsecond,
		RTTVariation: 62500 * time.Microsecond,
		BaseRTO:      time.Second,
		CurrentRTO:   time.Second,
		Samples:      2,
	})
	if !h.client.StreamCompleted() || h.transfer().DataRetransmissions != 2 {
		t.Fatalf("recovery outcome = %+v", h.client.GetState())
	}
}

// SYN retries at 1 s cannot yield an RTT sample from a SYN-ACK arriving at 1.5 s.
// Adaptive data then waits at least 3 s; fixed mode keeps its independent backoff policy.
func TestTCPRTOAfterSYNRetry(t *testing.T) {
	for _, test := range []struct {
		name        string
		mode        model.RTOMode
		backoff     bool
		maximum     time.Duration
		dataTimeout time.Duration
	}{
		{"adaptive", model.RTOAdaptive, true, time.Minute, 3 * time.Second},
		{"adaptive without backoff", model.RTOAdaptive, false, time.Minute, 3 * time.Second},
		{"adaptive capped fallback", model.RTOAdaptive, true, 2 * time.Second, 2 * time.Second},
		{"fixed", model.RTOFixed, true, time.Minute, 2 * time.Second},
		{"fixed without backoff", model.RTOFixed, false, time.Minute, time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newAdaptiveTransferHarness(t)
			config := model.DefaultRTOConfig()
			config.Mode = test.mode
			config.ExponentialBackoff = test.backoff
			config.MaximumTimeout = test.maximum
			h.client.SetRTOConfig(config)
			h.client.SendCompleteStream(1, 0)
			h.client.EstablishConnection()

			h.runUntil(1500 * time.Millisecond)
			if h.count(model.FlagSYN) != 2 {
				t.Fatal("expected the original SYN and its 1 s retry")
			}
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(h.engine.Now())
			assertRTOState(t, h, model.RTOState{
				BaseRTO:    time.Second,
				CurrentRTO: test.dataTimeout,
			})

			dataDeadline := h.engine.Now() + test.dataTimeout
			h.runUntil(dataDeadline - time.Nanosecond)
			if got := len(sentClientData(h.sent)); got != 1 {
				t.Fatalf("data retried before its %s timeout: %d transmissions", test.dataTimeout, got)
			}
			h.runUntil(dataDeadline)
			if len(sentClientData(h.sent)) != 2 || h.count(model.FlagSYN) != 2 {
				t.Fatalf("handshake timer or first data timeout is incorrect: %+v", h.sent)
			}
		})
	}
}

// Stopping at 150 ms cancels a pending SYN or data retry and rejects later ACK samples.
// With idle timeout disabled, no scheduled work may remain after cleanup.
func TestTCPRTOCleanupCancelsTimersAndProbe(t *testing.T) {
	for _, action := range []string{"handshake shutdown", "data shutdown", "manual close", "peer FIN"} {
		t.Run(action, func(t *testing.T) {
			h := newAdaptiveTransferHarness(t)
			h.client.SetIdleTimeout(0)
			h.client.SendCompleteStream(2048, 100*time.Millisecond)
			h.client.EstablishConnection()
			h.runUntil(0)
			if action != "handshake shutdown" {
				h.receive(model.FlagSYN|model.FlagACK, 1)
			}
			h.runUntil(150 * time.Millisecond)
			beforeStop := h.client.GetRTOState()
			payloadTransmissions := len(sentClientData(h.sent))

			switch action {
			case "manual close":
				h.client.Close()
			case "peer FIN":
				h.receive(model.FlagFIN|model.FlagACK, 1)
			default:
				h.client.Shutdown()
			}
			// Flush the optional FIN at the same time, then deliver a stale reply.
			h.runUntil(h.engine.Now())
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.receive(model.FlagACK, 1025)
			h.runUntil(5 * time.Second)

			assertRTOState(t, h, beforeStop)
			if len(sentClientData(h.sent)) != payloadTransmissions || h.count(model.FlagSYN) != 1 {
				t.Fatalf("stop left an active retransmission: %+v", h.sent)
			}
			if pending := h.engine.Pending(); pending != 0 {
				t.Fatalf("stop left %d scheduled events", pending)
			}
		})
	}
}
