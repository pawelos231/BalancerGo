package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"load-balancer/scenario"
)

// Contract examples preserve their timestamps, profiles and explicit overrides
// when decoded and written again, before execution.
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

			// The complete timeline validates before runtime admission.
			resolved, err := scenario.NormalizeV2(input)
			if err != nil {
				t.Fatalf("example failed timeline validation: %v", err)
			}
			if len(resolved.States) != len(input.States) || resolved.States[0].At != 0 {
				t.Fatal("normalized example lost its initial state or transitions")
			}

			// Compilation prepares every transition before world construction.
			plan, err := scenario.CompileV2(input)
			if err != nil {
				t.Fatalf("example failed timeline compilation: %v", err)
			}
			if _, err := plan.StateAt(input.Fixed.Execution.Duration); err != nil {
				t.Fatalf("compiled example cannot describe its terminal configuration: %v", err)
			}
		})
	}
}
