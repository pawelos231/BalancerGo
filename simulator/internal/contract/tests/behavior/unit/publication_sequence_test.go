package contract_test

import (
	"encoding/json"
	"testing"

	"load-balancer/internal/contract"
)

func TestPublicationSequenceWirePreservesDecimalText(t *testing.T) {
	for _, sequence := range []string{"1", "9007199254740993", "18446744073709551615"} {
		t.Run(sequence, func(t *testing.T) {
			metadata := contract.SimulationMetadata{
				Status: "running", RunID: "wire-run", PublicationSequence: sequence,
			}
			encoded, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if got, ok := wire["publicationSequence"].(string); !ok || got != sequence {
				t.Fatalf("publicationSequence = %#v, want JSON string %q", wire["publicationSequence"], sequence)
			}
			var restored contract.SimulationMetadata
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.PublicationSequence != sequence {
				t.Fatalf("round-trip sequence = %q, want %q", restored.PublicationSequence, sequence)
			}
		})
	}
}
