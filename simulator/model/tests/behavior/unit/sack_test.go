package model_test

import (
	"testing"

	"load-balancer/model"
)

// SACK changes header size, while sequence positions still count only SYN/FIN and payload.
func TestSACKOptionsWireSize(t *testing.T) {
	cases := []struct {
		name   string
		packet model.Packet
		wire   int
		end    uint64
	}{
		{
			name:   "ordinary payload keeps its old size",
			packet: model.Packet{Flag: model.FlagACK, Seq: 1, Size: 1024},
			wire:   1064,
			end:    1025,
		},
		{
			name:   "SYN permission is two option bytes plus padding",
			packet: model.Packet{Flag: model.FlagSYN, SACKPermitted: true},
			wire:   44,
			end:    1,
		},
		{
			name: "one block is ten option bytes plus padding",
			packet: model.Packet{
				Flag: model.FlagACK, Seq: 1, Ack: 1,
				SACKBlocks: []model.SACKBlock{{Start: 1025, End: 2049}},
			},
			wire: 52,
			end:  1,
		},
		{
			name: "four blocks fit without other options",
			packet: model.Packet{
				Flag: model.FlagACK, Seq: 1, Ack: 1,
				SACKBlocks: []model.SACKBlock{
					{Start: 2, End: 3}, {Start: 4, End: 5},
					{Start: 6, End: 7}, {Start: 8, End: 9},
				},
			},
			wire: 76,
			end:  1,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if !test.packet.ValidSACKOptions() {
				t.Fatal("valid packet rejected")
			}
			if got := test.packet.WireSize(); got != test.wire {
				t.Fatalf("wire size = %d, want %d", got, test.wire)
			}
			end, valid := test.packet.SequenceEnd()
			if !valid || end != test.end {
				t.Fatalf("sequence end = %d, %v; want %d, true", end, valid, test.end)
			}
		})
	}
}

// Malformed options are rejected before they can alter connection state.
func TestInvalidSACKOptions(t *testing.T) {
	block := model.SACKBlock{Start: 5, End: 9}
	cases := []struct {
		name   string
		packet model.Packet
	}{
		{
			name:   "permission outside SYN",
			packet: model.Packet{Flag: model.FlagACK, SACKPermitted: true},
		},
		{
			name:   "blocks on SYN",
			packet: model.Packet{Flag: model.FlagSYN, SACKBlocks: []model.SACKBlock{block}},
		},
		{
			name:   "blocks without ACK",
			packet: model.Packet{SACKBlocks: []model.SACKBlock{block}},
		},
		{
			name: "blocks on RST",
			packet: model.Packet{
				Flag: model.FlagRST | model.FlagACK, SACKBlocks: []model.SACKBlock{block},
			},
		},
		{
			name: "empty range",
			packet: model.Packet{
				Flag: model.FlagACK, SACKBlocks: []model.SACKBlock{{Start: 9, End: 9}},
			},
		},
		{
			name: "backwards range",
			packet: model.Packet{
				Flag: model.FlagACK, SACKBlocks: []model.SACKBlock{{Start: 9, End: 5}},
			},
		},
		{
			name: "too many blocks",
			packet: model.Packet{
				Flag: model.FlagACK, SACKBlocks: []model.SACKBlock{block, block, block, block, block},
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.packet.ValidSACKOptions() {
				t.Fatal("malformed options were accepted")
			}
		})
	}
}
