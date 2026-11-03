package tcp

import (
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	congestioncontrol "load-balancer/protocols/tcp/congestion_control"
)

// deadlinePolicy changes only the next-send deadline of a real window policy.
// It is a test stand-in for later paced algorithms, not a selectable TCP profile.
type deadlinePolicy struct {
	congestioncontrol.Policy
	deadline      time.Duration
	spacing       time.Duration
	deadlineOnACK time.Duration
}

func (policy *deadlinePolicy) OnACK(ack congestioncontrol.ACK) congestioncontrol.ACKResponse {
	response := policy.Policy.OnACK(ack)
	policy.deadline = policy.deadlineOnACK
	return response
}

func (policy *deadlinePolicy) OnSent(now time.Duration, payloadBytes int) {
	policy.Policy.OnSent(now, payloadBytes)
	policy.deadline = now + policy.spacing
}

func (policy *deadlinePolicy) State() congestioncontrol.State {
	state := policy.Policy.State()
	state.NextSendAt = policy.deadline
	return state
}

// The first packet obeys both gap and pacing; its ACK can move the second slot either way.
// A deadline beyond the horizon must leave the stream unfinished rather than sending early.
func TestNewDataSchedulingHonorsChangingPolicyDeadline(t *testing.T) {
	tests := []struct {
		name            string
		initialDeadline time.Duration
		spacing         time.Duration
		deadlineOnACK   time.Duration
		wantSends       []time.Duration
	}{
		{
			name: "later ACK deadline", initialDeadline: 50 * time.Millisecond,
			spacing: 50 * time.Millisecond, deadlineOnACK: 250 * time.Millisecond,
			wantSends: []time.Duration{100 * time.Millisecond, 250 * time.Millisecond},
		},
		{
			name: "earlier ACK deadline", initialDeadline: 50 * time.Millisecond,
			spacing: 200 * time.Millisecond, deadlineOnACK: 150 * time.Millisecond,
			wantSends: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond},
		},
		{
			name: "pacing exceeds gap", initialDeadline: 150 * time.Millisecond,
			spacing: 50 * time.Millisecond, deadlineOnACK: 300 * time.Millisecond,
			wantSends: []time.Duration{150 * time.Millisecond, 300 * time.Millisecond},
		},
		{
			name: "deadline beyond horizon", initialDeadline: 50 * time.Millisecond,
			spacing: 50 * time.Millisecond, deadlineOnACK: 500 * time.Millisecond,
			wantSends: []time.Duration{100 * time.Millisecond},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := engine.New(400 * time.Millisecond)
			c := newTestSender(e)
			t.Cleanup(c.Shutdown)
			// Keep retransmissions outside this test's 400 ms scheduling horizon.
			rto := model.DefaultRTOConfig()
			rto.Mode = model.RTOFixed
			c.SetRTOConfig(rto)
			c.congestion.policy = &deadlinePolicy{
				Policy: c.congestion.policy, deadline: test.initialDeadline,
				spacing: test.spacing, deadlineOnACK: test.deadlineOnACK,
			}
			var sentAt []time.Duration
			c.SetSender(func(packet model.Packet) {
				if packet.Flag == model.FlagSYN {
					c.HandlePacket(model.Packet{
						Key: c.Key(), Flag: model.FlagSYN | model.FlagACK, Ack: 1,
						ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
					})
				}
				if packet.Size == 0 {
					return
				}
				sentAt = append(sentAt, e.Now())
				if len(sentAt) == 1 {
					// The ACK arrives while the second new-data event is already queued.
					e.ScheduleAfter(25*time.Millisecond, func() {
						c.HandlePacket(model.Packet{
							Key: c.Key(), Flag: model.FlagACK, Seq: 1, Ack: 1025,
							ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
						})
					})
				}
			})
			c.SetSource(newTestPayloadSource(e, 2*model.SEGMENT_SIZE, 100*time.Millisecond))
			c.EstablishConnection()
			if err := e.RunUntil(e.EndTime()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sentAt, test.wantSends) {
				t.Fatalf("new data sent at %v, want %v", sentAt, test.wantSends)
			}
		})
	}
}
