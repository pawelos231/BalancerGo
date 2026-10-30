package client_test

import (
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
)

type transferHarness struct {
	t      *testing.T
	engine *engine.Engine
	client *client.Client
	sent   []model.Packet
}

// Existing transport tests keep fixed 1 s timers while adaptive timing is tested separately.
func newTransferHarness(t *testing.T) *transferHarness {
	t.Helper()
	h := newAdaptiveTransferHarness(t)
	config := model.DefaultRTOConfig()
	config.Mode = model.RTOFixed
	config.ExponentialBackoff = false
	h.client.SetRTOConfig(config)
	return h
}

// Keep the constructor's adaptive defaults, including its first 1 s timeout.
func newAdaptiveTransferHarness(t *testing.T) *transferHarness {
	t.Helper()
	e := engine.New(3 * time.Minute)
	c := client.NewClient(e, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
	h := &transferHarness{t: t, engine: e, client: c}
	c.SetSender(func(pkt model.Packet) { h.sent = append(h.sent, pkt) })
	t.Cleanup(c.Shutdown)
	return h
}

func (h *transferHarness) runUntil(until time.Duration) {
	h.t.Helper()
	if err := h.engine.RunUntil(until); err != nil {
		h.t.Fatal(err)
	}
}

func (h *transferHarness) receive(flag model.Flag, nextByte uint64) {
	h.receiveWindow(flag, nextByte, model.DefaultReceiveBufferBytes)
}

func (h *transferHarness) receiveWindow(flag model.Flag, nextByte uint64, windowBytes uint16) {
	h.t.Helper()
	peerSequence := uint64(1)
	if flag&model.FlagSYN != 0 {
		peerSequence = 0
	}
	h.client.HandlePacket(model.Packet{
		Key:                h.client.Key(),
		Flag:               flag,
		Seq:                peerSequence,
		Ack:                nextByte,
		ReceiveWindowBytes: windowBytes,
	})
}

func (h *transferHarness) transfer() model.TransferState {
	h.t.Helper()
	state := h.client.GetTransferState()
	if snapshot := h.client.GetState().Transfer; snapshot != state {
		h.t.Fatalf("client snapshot transfer = %+v, getter = %+v", snapshot, state)
	}
	return state
}

func (h *transferHarness) count(flag model.Flag) int {
	count := 0
	for _, pkt := range h.sent {
		if pkt.Flag&flag != 0 {
			count++
		}
	}
	return count
}

func assertTransferDuration(t *testing.T, name string, got time.Duration, want time.Duration) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %s, want %s", name, got, want)
	}
}

// Handshake and payload clocks keep separate milestones, including the initial send gap.
func TestTransferTimingUsesHandshakeAndPayloadMilestones(t *testing.T) {
	h := newTransferHarness(t)
	h.runUntil(2 * time.Second)
	h.client.SendCompleteStream(model.SEGMENT_SIZE+7, 1500*time.Millisecond)
	h.client.EstablishConnection()
	initial := h.transfer()
	if initial.Status != model.TransferInProgress || initial.HandshakeCompleted || initial.Elapsed != 0 {
		t.Fatalf("newly requested transfer = %+v", initial)
	}

	h.runUntil(3250 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	state := h.transfer()
	if !state.HandshakeCompleted || state.SynRetransmissions != 1 || h.client.GetState().RetrySyn != 0 {
		t.Fatalf("completed handshake = %+v, retry SYN = %d", state, h.client.GetState().RetrySyn)
	}
	assertTransferDuration(t, "handshake duration", state.HandshakeDuration, 1250*time.Millisecond)
	assertTransferDuration(t, "elapsed after handshake", state.Elapsed, 1250*time.Millisecond)

	// Duplicate SYN ACKs and repeated requests cannot move either milestone.
	h.runUntil(3500 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.client.EstablishConnection()
	h.client.SendCompleteStream(1, 0)
	assertTransferDuration(t, "handshake after duplicate", h.transfer().HandshakeDuration, 1250*time.Millisecond)
	h.runUntil(4750 * time.Millisecond)
	if got := h.count(model.FlagPSH); got != 1 {
		t.Fatalf("payload transmissions after initial gap = %d, want 1", got)
	}
	assertTransferDuration(t, "elapsed after initial gap", h.transfer().Elapsed, 2750*time.Millisecond)
	h.runUntil(5 * time.Second)
	h.receive(model.FlagACK, 1025)
	h.runUntil(6500 * time.Millisecond)
	h.runUntil(6750 * time.Millisecond)
	h.receive(model.FlagACK, 1032)

	state = h.transfer()
	if state.Status != model.TransferSucceeded || state.FailureReason != "" || !h.client.StreamCompleted() {
		t.Fatalf("completed transfer = %+v", state)
	}
	assertTransferDuration(t, "total elapsed", state.Elapsed, 4750*time.Millisecond)
	// The initial gap is excluded; the gap between payload segments is included.
	assertTransferDuration(t, "payload duration", state.PayloadDuration, 2*time.Second)
	h.runUntil(7 * time.Second)
	if got := h.transfer(); got != state {
		t.Fatalf("completed transfer changed with time: before %+v, after %+v", state, got)
	}
}

// An empty stream or a one-byte immediate ACK may complete at the same virtual time.
func TestTransferAllowsZeroDurationSuccess(t *testing.T) {
	for _, size := range []int{0, 1} {
		name := "empty stream"
		if size > 0 {
			name = "immediate payload ACK"
		}
		t.Run(name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(size, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(0)
			if size > 0 {
				h.receive(model.FlagACK, 2)
			}
			state := h.transfer()
			if state.Status != model.TransferSucceeded || !state.HandshakeCompleted || state.Elapsed != 0 ||
				state.HandshakeDuration != 0 || state.PayloadDuration != 0 || state.FailureReason != "" {
				t.Fatalf("zero-duration transfer = %+v", state)
			}
			if got := h.count(model.FlagPSH); got != size {
				t.Fatalf("payload transmissions = %d, want %d", got, size)
			}
			copy := h.client.GetTransferState()
			copy.Status = model.TransferFailed
			copy.Elapsed = 100
			if got := h.transfer(); got != state {
				t.Fatalf("editing returned state changed client: %+v", got)
			}
		})
	}
}

// Two retries of the first range and one of the second remain three total retries.
func TestTransferRetriesAccumulateAcrossAcknowledgments(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(model.SEGMENT_SIZE+7, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(1100 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(3200 * time.Millisecond)
	if state := h.transfer(); state.SynRetransmissions != 1 || state.DataRetransmissions != 2 {
		t.Fatalf("retries before first ACK = %+v", state)
	}
	if got := h.client.GetState().RetryData; got != 2 {
		t.Fatalf("current segment retries = %d, want 2", got)
	}
	h.receive(model.FlagACK, 1025)
	h.receive(model.FlagACK, 1025)
	if state := h.transfer(); state.DataRetransmissions != 2 || h.client.GetState().RetryData != 0 {
		t.Fatalf("retries after first ACK = %+v, current = %d", state, h.client.GetState().RetryData)
	}
	h.runUntil(4300 * time.Millisecond)
	if state := h.transfer(); state.DataRetransmissions != 3 || h.client.GetState().RetryData != 1 {
		t.Fatalf("second segment retries = %+v, current = %d", state, h.client.GetState().RetryData)
	}
	h.receive(model.FlagACK, 1032)
	state := h.transfer()
	if state.Status != model.TransferSucceeded || state.SynRetransmissions != 1 ||
		state.DataRetransmissions != 3 ||
		h.client.GetState().RetrySyn != 0 || h.client.GetState().RetryData != 0 {
		t.Fatalf("retries after completion = %+v, client = %+v", state, h.client.GetState())
	}
	if h.count(model.FlagSYN) != 2 || h.count(model.FlagPSH) != 5 {
		t.Fatalf("transmissions do not match retries: %+v", h.sent)
	}
	assertTransferDuration(t, "payload including retries", state.PayloadDuration, 3100*time.Millisecond)
	h.client.Shutdown()
	if got := h.transfer(); got != state {
		t.Fatalf("shutdown reset transfer metrics: %+v", got)
	}
}

// Closing a partially confirmed stream preserves its 1024 confirmed bytes and failure reason.
func TestTransferFailuresRetainPartialProgress(t *testing.T) {
	tests := []struct {
		name   string
		reason model.TransferFailureReason
		close  func(*transferHarness)
	}{
		{"shutdown", model.TransferInterrupted, func(h *transferHarness) { h.client.Shutdown() }},
		{"balancer timeout", model.TransferBalancerTimeout, func(h *transferHarness) {
			h.client.ShutdownWithReason(model.TransferBalancerTimeout)
		}},
		{"backend removed", model.TransferBackendRemoved, func(h *transferHarness) {
			h.client.ShutdownWithReason(model.TransferBackendRemoved)
		}},
		{"manual close", model.TransferClosedEarly, func(h *transferHarness) { h.client.Close() }},
		{"remote FIN", model.TransferRemoteClosed, func(h *transferHarness) {
			h.receive(model.FlagFIN|model.FlagACK, 2049)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.runUntil(2 * time.Second)
			h.client.SendCompleteStream(2*model.SEGMENT_SIZE+11, 0)
			h.client.EstablishConnection()
			h.runUntil(2250 * time.Millisecond)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(3250 * time.Millisecond)
			h.receive(model.FlagACK, 1025)
			h.runUntil(3500 * time.Millisecond)
			test.close(h)
			state := h.transfer()
			if state.Status != model.TransferFailed || state.FailureReason != test.reason ||
				!state.HandshakeCompleted ||
				state.DataRetransmissions != 1 || state.PayloadDuration != 0 {
				t.Fatalf("failed partial transfer = %+v", state)
			}
			assertTransferDuration(t, "failed elapsed", state.Elapsed, 1500*time.Millisecond)
			assertTransferDuration(t, "retained handshake duration", state.HandshakeDuration, 250*time.Millisecond)
			if got := h.client.GetState().AcknowledgedPayloadBytes; got != model.SEGMENT_SIZE {
				t.Fatalf("partial acknowledged payload = %d, want %d", got, model.SEGMENT_SIZE)
			}
			if test.reason == model.TransferClosedEarly {
				if h.client.IsTerminated() {
					t.Fatal("manual Close skipped the existing FIN exchange")
				}
				h.runUntil(h.engine.Now())
				if h.count(model.FlagFIN) != 1 {
					t.Fatalf("manual Close sent %d FINs, want 1", h.count(model.FlagFIN))
				}
			}
			h.runUntil(30 * time.Second)
			h.client.ShutdownWithReason(model.TransferInterrupted)
			h.receive(model.FlagACK, 2049)
			if got := h.transfer(); got != state {
				t.Fatalf("terminal failure changed: before %+v, after %+v", state, got)
			}
			if got := h.client.GetState().AcknowledgedPayloadBytes; got != model.SEGMENT_SIZE {
				t.Fatalf("late ACK changed partial payload to %d", got)
			}
		})
	}
}

// Manual Close during the handshake fails once and never starts a FIN exchange.
func TestTransferManualCloseBeforeHandshake(t *testing.T) {
	h := newTransferHarness(t)
	h.runUntil(time.Second)
	h.client.EstablishConnection()
	h.runUntil(1250 * time.Millisecond)
	h.client.Close()
	state := h.transfer()
	if !h.client.IsTerminated() || state.Status != model.TransferFailed ||
		state.FailureReason != model.TransferClosedEarly || state.HandshakeCompleted {
		t.Fatalf("close during handshake = %+v, terminated = %v", state, h.client.IsTerminated())
	}
	assertTransferDuration(t, "elapsed before early close", state.Elapsed, 250*time.Millisecond)
	h.runUntil(30 * time.Second)
	if got := h.transfer(); got != state || h.count(model.FlagFIN) != 0 {
		t.Fatalf("closed handshake changed: %+v, FINs = %d", got, h.count(model.FlagFIN))
	}
}

// Once the payload succeeds, losing the closing FIN reply cannot undo that success.
func TestTransferSuccessSurvivesFINTimeout(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(1, 0)
	h.client.EstablishConnection()
	h.runUntil(100 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(250 * time.Millisecond)
	h.receive(model.FlagACK, 2)
	state := h.transfer()
	if state.Status != model.TransferSucceeded || h.client.IsTerminated() {
		t.Fatalf("payload completion = %+v, terminated = %v", state, h.client.IsTerminated())
	}
	h.runUntil(h.engine.Now())
	if h.count(model.FlagFIN) != 1 {
		t.Fatalf("completion sent %d FINs, want 1", h.count(model.FlagFIN))
	}
	h.runUntil(250*time.Millisecond + model.READ_DEADLINE)
	if !h.client.IsTerminated() {
		t.Fatal("FIN read timeout did not close client")
	}
	h.client.ShutdownWithReason(model.TransferBackendRemoved)
	h.receive(model.FlagFIN|model.FlagACK, 3)
	if got := h.transfer(); got != state {
		t.Fatalf("FIN timeout changed successful transfer: before %+v, after %+v", state, got)
	}
}

// A silent peer eventually ends either handshake or payload with the same read deadline.
func TestTransferReadTimeout(t *testing.T) {
	for _, handshake := range []bool{false, true} {
		name := "handshake"
		if handshake {
			name = "payload"
		}
		t.Run(name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(1, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			if handshake {
				h.receive(model.FlagSYN|model.FlagACK, 1)
				h.runUntil(0)
			}
			h.runUntil(model.READ_DEADLINE)
			state := h.transfer()
			if !h.client.IsTerminated() || state.Status != model.TransferFailed ||
				state.FailureReason != model.TransferReadTimeout ||
				state.HandshakeCompleted != handshake || state.PayloadDuration != 0 {
				t.Fatalf("read timeout = %+v, terminated = %v", state, h.client.IsTerminated())
			}
			assertTransferDuration(t, "read timeout elapsed", state.Elapsed, model.READ_DEADLINE)
			if handshake {
				if state.DataRetransmissions == 0 || state.DataRetransmissions != uint64(h.count(model.FlagPSH)-1) {
					t.Fatalf("payload retries at timeout = %+v", state)
				}
			} else if state.SynRetransmissions == 0 || state.SynRetransmissions != uint64(h.count(model.FlagSYN)-1) {
				t.Fatalf("SYN retries at timeout = %+v", state)
			}
			h.runUntil(40 * time.Second)
			if got := h.transfer(); got != state {
				t.Fatalf("read timeout state changed: %+v", got)
			}
		})
	}
}

// Repeated silence exhausts the configured SYN or data retry budget exactly once.
func TestTransferRetryLimits(t *testing.T) {
	for _, payload := range []bool{false, true} {
		name := "SYN"
		timeout := model.SYN_TIMEOUT
		reason := model.TransferSynRetryLimit
		flag := model.FlagSYN
		if payload {
			name = "payload"
			timeout = model.SEGMENT_TIMEOUT
			reason = model.TransferDataRetryLimit
			flag = model.FlagPSH
		}
		t.Run(name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(1, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			if payload {
				h.receive(model.FlagSYN|model.FlagACK, 1)
				h.runUntil(0)
			}
			for retry := 1; retry <= model.MAX_RETRANSMISIONS+1; retry++ {
				at := time.Duration(retry) * timeout
				h.runUntil(at - time.Nanosecond)
				// Keep the independent read deadline from masking the retry limit.
				h.receive(0, 0)
				h.runUntil(at)
				if retry <= model.MAX_RETRANSMISIONS && h.transfer().Status != model.TransferInProgress {
					t.Fatalf("transfer ended before retry %d was exhausted: %+v", retry, h.transfer())
				}
			}
			state := h.transfer()
			if !h.client.IsTerminated() || state.Status != model.TransferFailed || state.FailureReason != reason {
				t.Fatalf("retry exhaustion = %+v, terminated = %v", state, h.client.IsTerminated())
			}
			assertTransferDuration(t, "retry limit elapsed", state.Elapsed, (model.MAX_RETRANSMISIONS+1)*timeout)
			if got := h.count(flag); got != model.MAX_RETRANSMISIONS+1 {
				t.Fatalf("transmissions = %d, want one original plus %d retries", got, model.MAX_RETRANSMISIONS)
			}
			if payload {
				if state.DataRetransmissions != model.MAX_RETRANSMISIONS || state.SynRetransmissions != 0 {
					t.Fatalf("payload retry total = %+v", state)
				}
			} else if state.SynRetransmissions != model.MAX_RETRANSMISIONS || state.DataRetransmissions != 0 {
				t.Fatalf("SYN retry total = %+v", state)
			}
			h.runUntil(50 * time.Second)
			h.client.Shutdown()
			if got := h.transfer(); got != state {
				t.Fatalf("retry failure state changed: %+v", got)
			}
		})
	}
}
