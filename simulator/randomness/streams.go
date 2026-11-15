package randomness

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand"
	"strings"
)

// StreamModel identifies seed derivation and the legacy math/rand generator.
// Existing algorithm, balancer and link streams retain their original seeds;
// other pairs use versioned, length-prefixed resource and phenomenon names.
const StreamModel = "resource-phenomenon-v1"

const (
	Selection  = "selection"
	PacketLoss = "packet-loss"
	Congestion = "congestion"
)

type streamKey struct {
	resourceID string
	phenomenon string
}

// Streams owns advancing random sequences for one simulation run.
// The DES owner goroutine serializes both lookups and draws. Separate streams
// isolate consumption; distinct keys do not promise distinct PRNG sequences.
type Streams struct {
	seed    int64
	streams map[streamKey]*rand.Rand
}

// NewStreams starts a fresh run with an exact signed master seed.
func NewStreams(seed int64) *Streams {
	return &Streams{seed: seed, streams: make(map[streamKey]*rand.Rand)}
}

// Stream returns the existing sequence for a resource and phenomenon, creating
// it without drawing from any stream. Empty names are programmer errors.
func (streams *Streams) Stream(resourceID, phenomenon string) *rand.Rand {
	if resourceID == "" || phenomenon == "" {
		panic("randomness: resource ID and phenomenon must not be empty")
	}
	key := streamKey{resourceID: resourceID, phenomenon: phenomenon}
	if stream, exists := streams.streams[key]; exists {
		return stream
	}
	stream := rand.New(rand.NewSource(streams.streamSeed(key)))
	streams.streams[key] = stream
	return stream
}

func (streams *Streams) streamSeed(key streamKey) int64 {
	hash := fnv.New64a()
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], uint64(streams.seed))
	_, _ = hash.Write(encoded[:])
	if id, legacy := legacyStreamID(key); legacy {
		_, _ = hash.Write([]byte(id))
	} else {
		for _, part := range []string{StreamModel, key.resourceID, key.phenomenon} {
			binary.LittleEndian.PutUint64(encoded[:], uint64(len(part)))
			_, _ = hash.Write(encoded[:])
			_, _ = hash.Write([]byte(part))
		}
	}
	return int64(hash.Sum64())
}

// Legacy bindings retain the realized routing, loss and signaling results of
// scenarios predating the registry. The map itself always uses the full tuple.
func legacyStreamID(key streamKey) (string, bool) {
	if key.resourceID == "algorithm" && key.phenomenon == Selection {
		return "algorithm", true
	}
	if key.resourceID == "balancer" && key.phenomenon == PacketLoss {
		return "balancer", true
	}
	if strings.HasPrefix(key.resourceID, "link/") {
		switch key.phenomenon {
		case PacketLoss:
			return key.resourceID, true
		case Congestion:
			return key.resourceID + "/congestion", true
		}
	}
	return "", false
}
