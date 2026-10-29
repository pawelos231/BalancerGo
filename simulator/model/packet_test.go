package model_test

import (
	"testing"

	"load-balancer/model"
)

// Payload and SYN/FIN share sequence space; pure ACKs consume no positions.
func TestPacketSequenceEnd(t *testing.T) {
	maximum := ^uint64(0)
	cases := []struct {
		name   string
		packet model.Packet
		end    uint64
		valid  bool
	}{
		{
			name:   "pure ACK",
			packet: model.Packet{Flag: model.FlagACK, Seq: 100, Ack: 900},
			end:    100,
			valid:  true,
		},
		{
			name:   "SYN",
			packet: model.Packet{Flag: model.FlagSYN, Seq: 100},
			end:    101,
			valid:  true,
		},
		{
			name:   "short payload",
			packet: model.Packet{Flag: model.FlagACK, Seq: 2049, Size: 2},
			end:    2051,
			valid:  true,
		},
		{
			name:   "FIN",
			packet: model.Packet{Flag: model.FlagFIN | model.FlagACK, Seq: 2051},
			end:    2052,
			valid:  true,
		},
		{
			name:   "logical range beyond 32 bits",
			packet: model.Packet{Seq: 1 << 32, Size: 1024},
			end:    1<<32 + 1024,
			valid:  true,
		},
		{
			name:   "maximum pure ACK",
			packet: model.Packet{Flag: model.FlagACK, Seq: maximum},
			end:    maximum,
			valid:  true,
		},
		{
			name:   "negative payload",
			packet: model.Packet{Size: -1},
		},
		{
			name:   "payload overflow",
			packet: model.Packet{Seq: maximum, Size: 1},
		},
		{
			name:   "SYN overflow",
			packet: model.Packet{Flag: model.FlagSYN, Seq: maximum},
		},
		{
			name:   "FIN overflow",
			packet: model.Packet{Flag: model.FlagFIN, Seq: maximum},
		},
		{
			name:   "payload and FIN overflow",
			packet: model.Packet{Flag: model.FlagFIN, Seq: maximum - 1, Size: 1},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			end, valid := test.packet.SequenceEnd()
			if end != test.end || valid != test.valid {
				t.Fatalf("sequence end = %d, %v; want %d, %v", end, valid, test.end, test.valid)
			}
		})
	}
}
