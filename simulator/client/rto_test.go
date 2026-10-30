package client_test

import (
	"math"
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
)

// RTT samples of 400 ms and 600 ms produce adaptive timeouts of 1.2 s and 1.225 s.
// Fixed mode measures the same RTTs while keeping its configured 2 s base.
func TestTCPRTOEstimatesRoundTrips(t *testing.T) {
	for _, mode := range []model.RTOMode{model.RTOAdaptive, model.RTOFixed} {
		t.Run(string(mode), func(t *testing.T) {
			h := newAdaptiveTransferHarness(t)
			if mode == model.RTOFixed {
				config := model.DefaultRTOConfig()
				config.Mode = model.RTOFixed
				config.InitialTimeout = 2 * time.Second
				h.client.SetRTOConfig(config)
			}

			h.client.EstablishConnection()
			h.runUntil(400 * time.Millisecond)
			h.receive(model.FlagSYN|model.FlagACK, 1)

			firstTimeout := 1200 * time.Millisecond
			if mode == model.RTOFixed {
				firstTimeout = 2 * time.Second
			}
			assertRTOState(t, h, model.RTOState{
				LatestRTT:    400 * time.Millisecond,
				SmoothedRTT:  400 * time.Millisecond,
				RTTVariation: 200 * time.Millisecond,
				BaseRTO:      firstTimeout,
				CurrentRTO:   firstTimeout,
				Samples:      1,
			})

			// Both segments leave at 400 ms; the first is confirmed 600 ms later.
			h.client.SendCompleteStream(2048, 0)
			h.runUntil(time.Second)
			h.receive(model.FlagACK, 1025)

			secondTimeout := 1225 * time.Millisecond
			if mode == model.RTOFixed {
				secondTimeout = 2 * time.Second
			}
			want := model.RTOState{
				LatestRTT:    600 * time.Millisecond,
				SmoothedRTT:  425 * time.Millisecond,
				RTTVariation: 200 * time.Millisecond,
				BaseRTO:      secondTimeout,
				CurrentRTO:   secondTimeout,
				Samples:      2,
			}
			assertRTOState(t, h, want)

			// The second segment had no separate probe, so its ACK adds no sample.
			h.runUntil(1100 * time.Millisecond)
			h.receive(model.FlagACK, 2049)
			assertRTOState(t, h, want)
		})
	}
}

// SYN and three data segments acknowledged synchronously at t=0 give four zero RTTs.
// A measured zero still uses the default minimum timeout of 200 ms.
func TestTCPRTOAcceptsSynchronousZeroRoundTrips(t *testing.T) {
	h := newAdaptiveTransferHarness(t)
	assertRTOState(t, h, model.RTOState{BaseRTO: time.Second, CurrentRTO: time.Second})

	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		switch {
		case packet.Flag == model.FlagSYN:
			h.receive(model.FlagSYN|model.FlagACK, 1)
		case packet.Size > 0:
			// Immediate replies must see the probe and pending range already recorded.
			h.receive(model.FlagACK, packet.Seq+uint64(packet.Size))
		}
	})

	h.client.SendCompleteStream(2050, 0)
	h.client.EstablishConnection()
	h.runUntil(2 * time.Second)
	assertRTOState(t, h, model.RTOState{
		BaseRTO:    200 * time.Millisecond,
		CurrentRTO: 200 * time.Millisecond,
		Samples:    4,
	})
	if !h.client.StreamCompleted() || len(sentClientData(h.sent)) != 3 {
		t.Fatalf("synchronous transfer = %+v, packets = %+v", h.client.GetState(), h.sent)
	}
	if h.transfer().SynRetransmissions != 0 || h.transfer().DataRetransmissions != 0 {
		t.Fatal("completed synchronous transfer kept a retransmission timer")
	}
}

// RTT=100 ms gives a raw timeout of 300 ms, bounded here to 500 ms.
// RTT=900 ms gives 2.7 s, bounded instead to the configured maximum of 2 s.
func TestTCPRTOClampsMeasuredTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		rtt     time.Duration
		timeout time.Duration
	}{
		{name: "minimum", rtt: 100 * time.Millisecond, timeout: 500 * time.Millisecond},
		{name: "maximum", rtt: 900 * time.Millisecond, timeout: 2 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newAdaptiveTransferHarness(t)
			config := model.DefaultRTOConfig()
			config.MinimumTimeout = 500 * time.Millisecond
			config.MaximumTimeout = 2 * time.Second
			h.client.SetRTOConfig(config)

			h.client.EstablishConnection()
			h.runUntil(test.rtt)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			state := h.client.GetRTOState()
			if state.Samples != 1 || state.BaseRTO != test.timeout || state.CurrentRTO != test.timeout {
				t.Fatalf("bounded RTT=%s state = %+v, want timeout %s", test.rtt, state, test.timeout)
			}
		})
	}
}

// A 146-year RTT or doubled timeout exceeds time.Duration's range and must saturate.
// Neither calculation may wrap into a negative timeout or schedule an event in the past.
func TestTCPRTOHandlesDurationLimits(t *testing.T) {
	const maximumDuration = time.Duration(math.MaxInt64)
	for _, measure := range []bool{true, false} {
		name := "backoff saturation"
		if measure {
			name = "RTT margin saturation"
		}
		t.Run(name, func(t *testing.T) {
			e := engine.New(maximumDuration)
			c := client.NewClient(e, "10.0.0.1", 1000, "10.0.0.2", 80, model.TCP)
			h := &transferHarness{t: t, engine: e, client: c}
			c.SetSender(func(packet model.Packet) { h.sent = append(h.sent, packet) })
			t.Cleanup(c.Shutdown)

			config := model.DefaultRTOConfig()
			config.InitialTimeout = maximumDuration
			config.MaximumTimeout = maximumDuration
			if !measure {
				config.InitialTimeout = maximumDuration/2 + 1
			}
			c.SetRTOConfig(config)
			c.SetIdleTimeout(0)
			c.EstablishConnection()

			if measure {
				h.runUntil(maximumDuration / 2)
				h.receive(model.FlagSYN|model.FlagACK, 1)
				// A second very large sample also exercises smoothing without weighted-sum overflow.
				c.SendCompleteStream(1, 0)
				h.runUntil(maximumDuration - 1)
				h.receive(model.FlagACK, 2)
			} else {
				h.runUntil(config.InitialTimeout)
			}

			state := c.GetRTOState()
			wantBase := maximumDuration
			if !measure {
				wantBase = config.InitialTimeout
			}
			if state.BaseRTO != wantBase || state.CurrentRTO != maximumDuration {
				t.Fatalf("duration limit produced an invalid timeout: %+v", state)
			}
			if measure && (state.Samples != 2 || state.SmoothedRTT != maximumDuration/2) {
				t.Fatalf("large RTT samples overflowed smoothing: %+v", state)
			}
		})
	}
}

// Compare the public getter and snapshot, so both expose the same timeout measurements.
func assertRTOState(t *testing.T, h *transferHarness, want model.RTOState) {
	t.Helper()
	got := h.client.GetRTOState()
	if got != want {
		t.Fatalf("RTO state = %+v, want %+v", got, want)
	}
	if snapshot := h.client.GetState().RTO; snapshot != got {
		t.Fatalf("RTO snapshot = %+v, getter = %+v", snapshot, got)
	}
}
