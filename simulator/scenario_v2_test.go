package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"load-balancer/scenario"
)

// Contract examples preserve their timestamps, profiles and explicit overrides
// when decoded and written again, without needing a v2 simulation executor.
func TestScenarioV2ExamplesRoundTrip(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "examples", "scenarios-v2", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no v2 contract examples found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			document, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			input, err := scenario.DecodeDocumentV2(document)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := scenario.EncodeDocumentV2(input)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := scenario.DecodeDocumentV2(encoded)
			if err != nil || !reflect.DeepEqual(restored, input) {
				t.Fatalf("example changed during round trip: %v", err)
			}
		})
	}
}

// The new reader can inspect v2, but existing entry points must reject the entire
// document instead of quietly running its first state through the v1 executor.
func TestScenarioV2CannotRunBeforeRuntimeMigration(t *testing.T) {
	path := filepath.Join("..", "examples", "scenarios-v2", "static.json")
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.DecodeDocumentV2(document); err != nil {
		t.Fatalf("v2 example is not readable by its own codec: %v", err)
	}

	t.Run("HTTP", func(t *testing.T) {
		h := newScenarioHarness(t)
		for _, endpoint := range []string{"/api/scenarios/validate", "/api/simulations"} {
			scenarioRequest(t, h.server, http.MethodPost, endpoint, document, http.StatusBadRequest)
		}
		scenarioRequest(t, h.server, http.MethodGet, "/api/simulations/current", nil, http.StatusNotFound)
		if h.control.currentRun() != nil || h.control.contextForRun() != nil || len(h.control.starts) != 0 {
			t.Fatal("v2 document reserved or created a run through the old HTTP entry point")
		}
	})

	t.Run("CLI", func(t *testing.T) {
		var output bytes.Buffer
		err := runScenarioFile(context.Background(), configuration{scenarioPath: path}, &output)
		if err == nil || output.Len() != 0 {
			t.Fatalf("v2 must fail before execution or report output: error=%v output=%s", err, output.Bytes())
		}
	})
}
