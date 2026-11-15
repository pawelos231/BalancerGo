package randomness_test

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"load-balancer/randomness"
)

func TestStreamLookupContinuesItsSequence(t *testing.T) {
	streams := randomness.NewStreams(41)
	stream := streams.Stream("algorithm", randomness.Selection)
	reference := randomness.NewStreams(41).Stream("algorithm", randomness.Selection)
	for index := range 32 {
		if lookup := streams.Stream("algorithm", randomness.Selection); lookup != stream {
			t.Fatal("a repeated lookup replaced the owned stream")
		}
		if got, want := stream.Uint64(), reference.Uint64(); got != want {
			t.Fatalf("draw %d = %d, want %d", index, got, want)
		}
	}
}

func TestStreamCreationOrderDoesNotChangeSequences(t *testing.T) {
	keys := [][2]string{
		{"algorithm", randomness.Selection},
		{"balancer", randomness.PacketLoss},
		{"link/client/alpha/0/to-lb", randomness.PacketLoss},
		{"link/client/alpha/0/to-lb", randomness.Congestion},
		{"group/alpha", "arrival"},
		{"group/alpha", "payload-size"},
		{"group/beta", "arrival"},
	}
	forward, reverse := randomness.NewStreams(19), randomness.NewStreams(19)
	first, second := make([]*rand.Rand, len(keys)), make([]*rand.Rand, len(keys))
	for index, key := range keys {
		first[index] = forward.Stream(key[0], key[1])
	}
	for index := len(keys) - 1; index >= 0; index-- {
		key := keys[index]
		second[index] = reverse.Stream(key[0], key[1])
	}
	for draw := range 64 {
		for index, key := range keys {
			if got, want := second[index].Uint64(), first[index].Uint64(); got != want {
				t.Fatalf("%v draw %d = %d, want %d", key, draw, got, want)
			}
		}
	}
}

func TestDifferentPhenomenaAndResourcesIsolateConsumption(t *testing.T) {
	streams, reference := randomness.NewStreams(5), randomness.NewStreams(5)
	loss := streams.Stream("link/shared", randomness.PacketLoss)
	control := reference.Stream("link/shared", randomness.PacketLoss)
	others := []*rand.Rand{
		streams.Stream("link/shared", randomness.Congestion),
		streams.Stream("link/other", randomness.PacketLoss),
		streams.Stream("link/shared", "future-phenomenon"),
	}
	for _, other := range others {
		if other == loss {
			t.Fatal("different resource/phenomenon pairs share a random source")
		}
	}
	for draw := range 64 {
		for _, other := range others {
			for range draw + 1 {
				other.Float64()
			}
		}
		if got, want := loss.Float64(), control.Float64(); got != want {
			t.Fatalf("other resource or phenomenon consumed loss draw %d", draw)
		}
	}
}

// These legacy names intentionally derive the same seed. Ownership must still
// use the tuple so consuming one stream cannot consume the other stream.
func TestLegacySeedAliasesStillOwnSeparateStreams(t *testing.T) {
	streams := randomness.NewStreams(7)
	congestion := streams.Stream("link/a", randomness.Congestion)
	loss := streams.Stream("link/a/congestion", randomness.PacketLoss)
	control := randomness.NewStreams(7).Stream("link/a/congestion", randomness.PacketLoss)
	if congestion == loss {
		t.Fatal("legacy seed alias replaced tuple-based ownership")
	}
	for range 64 {
		congestion.Uint64()
	}
	if got, want := loss.Uint64(), control.Uint64(); got != want {
		t.Fatal("a stream with the same legacy seed consumed another tuple's draw")
	}
}

func TestStreamsReplayTheFullSignedSeedRange(t *testing.T) {
	// Include signed limits, zero, and values outside JavaScript's exact range.
	for _, seed := range []int64{
		math.MinInt64,
		-9_007_199_254_740_993,
		-1,
		0,
		1,
		9_007_199_254_740_993,
		math.MaxInt64,
	} {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			first, second := randomness.NewStreams(seed), randomness.NewStreams(seed)
			for _, key := range [][2]string{
				{"algorithm", randomness.Selection},
				{"link/a", randomness.PacketLoss},
				{"link/a", randomness.Congestion},
				{"group/a", "arrival"},
			} {
				for draw := range 32 {
					if got, want := first.Stream(key[0], key[1]).Uint64(), second.Stream(key[0], key[1]).Uint64(); got != want {
						t.Fatalf("%v draw %d is not reproducible", key, draw)
					}
				}
			}
		})
	}
}

// Captured from the pre-registry World.resourceRNG FNV derivation and Go's
// legacy math/rand source, independently of the stream registry implementation.
func TestStreamsRetainLegacyGoldenSequences(t *testing.T) {
	for _, test := range []struct {
		seed       int64
		resource   string
		phenomenon string
		want       [3]int64
	}{
		{
			seed:       1,
			resource:   "algorithm",
			phenomenon: randomness.Selection,
			want:       [3]int64{8374192550572218796, 1516288397128073582, 330493537585485855},
		},
		{
			seed:       1,
			resource:   "balancer",
			phenomenon: randomness.PacketLoss,
			want:       [3]int64{2446533230781911237, 1565835179125861514, 7530425801763874463},
		},
		{
			seed:       1,
			resource:   "link/infrastructure/ingress",
			phenomenon: randomness.PacketLoss,
			want:       [3]int64{1701890179771407100, 4899301773222605262, 6865282246121834514},
		},
		{
			seed:       1,
			resource:   "link/backend/primary/to-lb",
			phenomenon: randomness.PacketLoss,
			want:       [3]int64{6203690479473258752, 1895631703599403615, 3119750618333673921},
		},
		{
			seed:       1,
			resource:   "link/backend/primary/to-lb",
			phenomenon: randomness.Congestion,
			want:       [3]int64{2441971715941947259, 5494530301681811779, 6684608852253332483},
		},
		{
			seed:       -7,
			resource:   "algorithm",
			phenomenon: randomness.Selection,
			want:       [3]int64{3071911995261574406, 8773548832311407070, 4986381043820821453},
		},
		{
			seed:       math.MinInt64,
			resource:   "balancer",
			phenomenon: randomness.PacketLoss,
			want:       [3]int64{3376673635115531215, 5282862476050620757, 4237200653831640500},
		},
		{
			seed:       math.MaxInt64,
			resource:   "link/client/alpha/0/from-lb",
			phenomenon: randomness.PacketLoss,
			want:       [3]int64{8534553447684338506, 5503017696814395568, 754770794719741022},
		},
	} {
		t.Run(strconv.FormatInt(test.seed, 10)+"/"+test.resource+"/"+test.phenomenon, func(t *testing.T) {
			stream := randomness.NewStreams(test.seed).Stream(test.resource, test.phenomenon)
			for draw, want := range test.want {
				if got := stream.Int63(); got != want {
					t.Fatalf("legacy draw %d = %d, want %d", draw, got, want)
				}
			}
		})
	}
}

func TestNewTupleEncodingHasStableUnambiguousBoundaries(t *testing.T) {
	if randomness.StreamModel != "resource-phenomenon-v1" {
		t.Fatal("update the golden vectors when the stream model changes")
	}
	streams := randomness.NewStreams(1)
	// Concatenation and NUL delimiters would confuse these pairs; length prefixes
	// preserve each resource and phenomenon boundary.
	for _, test := range []struct {
		resource   string
		phenomenon string
		want       [3]int64
	}{
		{
			resource:   "ab",
			phenomenon: "c",
			want:       [3]int64{8090479971620653102, 8768923423242105235, 1785793451100148371},
		},
		{
			resource:   "a",
			phenomenon: "bc",
			want:       [3]int64{465521935492908004, 7757733922831332897, 1507234707607270738},
		},
		{
			resource:   "a\x00b",
			phenomenon: "c",
			want:       [3]int64{5081695283319871677, 8042260935982506773, 6290167078646995743},
		},
		{
			resource:   "a",
			phenomenon: "b\x00c",
			want:       [3]int64{1662520937473459884, 6917403123817018977, 6117417270279610664},
		},
	} {
		stream := streams.Stream(test.resource, test.phenomenon)
		for draw, want := range test.want {
			if got := stream.Int63(); got != want {
				t.Fatalf("tuple (%q, %q) draw %d = %d, want %d", test.resource, test.phenomenon, draw, got, want)
			}
		}
	}
}

func TestStreamsRejectEmptyNames(t *testing.T) {
	for _, key := range [][2]string{{"", randomness.Selection}, {"algorithm", ""}, {"", ""}} {
		t.Run(strconv.Quote(key[0])+"/"+strconv.Quote(key[1]), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("an empty resource or phenomenon did not panic")
				}
			}()
			randomness.NewStreams(1).Stream(key[0], key[1])
		})
	}
}
