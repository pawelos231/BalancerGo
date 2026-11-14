package scenario_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"load-balancer/scenario"
)

type presetBaseline struct {
	BaselineRevision string                       `json:"baselineRevision"`
	Presets          map[string]presetFingerprint `json:"presets"`
}

type presetFingerprint struct {
	NormalizedSHA256 string `json:"normalizedSha256"`
	SelectorsSHA256  string `json:"selectorsSha256"`
}

// The fixture was frozen from the actual UI factories at 72ba400, before their
// replacement with language sources. Hash every resolved field without filters.
func TestPresetSourcesPreserveNativeConfiguration(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "fixtures", "preset-normalized-baseline.json")
	document, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var baseline presetBaseline
	if err := json.Unmarshal(document, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.BaselineRevision != "72ba400" || len(baseline.Presets) != 37 {
		t.Fatalf("expected the 37 presets frozen at 72ba400, got revision %q and %d presets", baseline.BaselineRevision, len(baseline.Presets))
	}

	root := filepath.Join("..", "..", "..", "..", "..")
	ids := make([]string, 0, len(baseline.Presets))
	for id := range baseline.Presets {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// A source added or removed from the catalog needs an explicit parity decision.
	sourcePaths, err := filepath.Glob(filepath.Join(root, "visualizer", "src", "scenarios", "presets", "sources", "*.lb"))
	if err != nil {
		t.Fatal(err)
	}
	sourceIDs := make([]string, 0, len(sourcePaths))
	for _, path := range sourcePaths {
		sourceIDs = append(sourceIDs, strings.TrimSuffix(filepath.Base(path), ".lb"))
	}
	sort.Strings(sourceIDs)
	if !reflect.DeepEqual(sourceIDs, ids) {
		t.Fatalf("preset source IDs differ from the frozen baseline:\n sources: %v\n baseline: %v", sourceIDs, ids)
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			document, err := os.ReadFile(filepath.Join(root, "examples", "scenarios", id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			input, err := scenario.DecodeDocument(document)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := scenario.Normalize(input)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := scenario.EncodeNormalizedDocument(resolved)
			if err != nil {
				t.Fatal(err)
			}
			expected := baseline.Presets[id]
			if actual := presetSHA256(encoded); actual != expected.NormalizedSHA256 {
				t.Errorf("complete normalized configuration changed from %s: SHA256 %s, want %s", baseline.BaselineRevision, actual, expected.NormalizedSHA256)
			}

			// Normalization resolves profiles and patches. Preserve their native
			// selectors separately so equivalent numbers cannot hide their loss.
			selectors, err := presetSelectors(input)
			if err != nil {
				t.Fatal(err)
			}
			if actual := presetSHA256(selectors); actual != expected.SelectorsSHA256 {
				t.Errorf("native profiles, network selectors or stream patches changed from %s: SHA256 %s, want %s", baseline.BaselineRevision, actual, expected.SelectorsSHA256)
			}
		})
	}
}

func presetSHA256(document []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(document))
}

// Keep this projection small: the complete normalized hash already covers
// numeric settings, identities, membership, ordering and timeline timestamps.
func presetSelectors(input scenario.Scenario) ([]byte, error) {
	states := make(map[string]any, len(input.States))
	for at, state := range input.States {
		backends := make([]map[string]any, 0, len(state.Resources.Backends))
		for _, backend := range state.Resources.Backends {
			backends = append(backends, map[string]any{"id": backend.ID, "network": backend.Network})
		}
		groups := make([]map[string]any, 0, len(state.Workload.ClientGroups))
		for _, group := range state.Workload.ClientGroups {
			overrides := make([]map[string]any, 0, len(group.Overrides))
			for _, override := range group.Overrides {
				overrides = append(overrides, map[string]any{
					"index": override.Index, "network": override.Network, "stream": override.Stream,
				})
			}
			groups = append(groups, map[string]any{
				"id": group.ID, "network": group.Network, "stream": group.Stream, "overrides": overrides,
			})
		}
		states[at] = map[string]any{"backends": backends, "clientGroups": groups}
	}
	return json.Marshal(map[string]any{"network": input.Fixed.Network, "states": states})
}
