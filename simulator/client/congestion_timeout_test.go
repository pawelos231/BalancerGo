package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// With cwnd=8192 but only 6144 bytes in flight, loss sets ssthresh=3072, not 4096.
// Repeated retries of the same original segment keep that threshold, even after a partial ACK.
func TestTCPRenoTimeoutUsesFlightAndKeepsPendingData(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = 8
	h.client.SetCongestionConfig(config)
	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 6144)
	h.runUntil(time.Second)

	state := congestionState(t, h)
	if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 3072 ||
		state.BytesInFlight != 6144 || len(sentClientData(h.sent)) != 7 {
		t.Fatalf("timeout discarded flight or halved cwnd instead of flight: %+v", state)
	}

	// Only half the first segment is confirmed; its 512-byte suffix still owns the loss.
	h.runUntil(1100 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 513, 6144)
	h.runUntil(2100 * time.Millisecond)
	data := sentClientData(h.sent)
	state = congestionState(t, h)
	if len(data) != 8 || data[7].Seq != 513 || data[7].Size != 512 {
		t.Fatalf("partial ACK allowed new data or retried the wrong fragment: %+v", data)
	}
	if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 3072 ||
		state.BytesInFlight != 5632 {
		t.Fatalf("retrying the same original segment halved its threshold twice: %+v", state)
	}

	// Finishing that original segment exposes a new head; its RTO halves flight=5120.
	h.runUntil(2200 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 1025, 6144)
	h.runUntil(3200 * time.Millisecond)
	state = congestionState(t, h)
	if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 2560 ||
		state.BytesInFlight != 5120 {
		t.Fatalf("new head did not recompute its loss threshold from current flight: %+v", state)
	}
	h.runUntil(4200 * time.Millisecond)
	if repeated := congestionState(t, h); repeated != state {
		t.Fatalf("another timeout of the same head changed its threshold or flight: %+v", repeated)
	}

	// Flight=2048 still fills cwnd=2048, so the first large ACK permits no new send.
	h.runUntil(4300 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 4097, 6144)
	h.runUntil(h.engine.Now())
	if got := len(sentClientData(h.sent)); got != 10 {
		t.Fatalf("sender exceeded its reduced window before credit was available: %d sends", got)
	}

	// Another ACK leaves 1024 in flight and cwnd=3072, releasing two original segments.
	h.runUntil(4400 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 5121, 6144)
	h.runUntil(h.engine.Now())
	data = sentClientData(h.sent)
	state = congestionState(t, h)
	if len(data) != 12 || data[10].Seq != 6145 || data[11].Seq != 7169 {
		t.Fatalf("freed congestion credit did not release the two unsent segments: %+v", data)
	}
	if state.CongestionWindowBytes != 3072 || state.BytesInFlight != 3072 {
		t.Fatalf("sender did not refill only the newly available credit: %+v", state)
	}
}

// Losing a single 1024-byte segment sets ssthresh to the two-MSS floor, 2048 bytes.
// Its unconfirmed bytes remain in flight while the window falls from 4096 to 1024.
func TestTCPRenoTimeoutKeepsTwoSegmentThresholdFloor(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	h.client.SendCompleteStream(1024, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(time.Second)

	state := congestionState(t, h)
	if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 2048 ||
		state.BytesInFlight != 1024 || state.Phase != "slowStart" {
		t.Fatalf("small flight did not keep the two-segment threshold floor: %+v", state)
	}
}

// A fixed 1 s RTO keeps congestion examples independent from RTT estimation and backoff.
func useFixedCongestionTestTimers(h *transferHarness) {
	h.t.Helper()
	config := model.DefaultRTOConfig()
	config.Mode = model.RTOFixed
	config.ExponentialBackoff = false
	h.client.SetRTOConfig(config)
	h.client.SetIdleTimeout(0)
}
