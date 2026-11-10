package scenario_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"load-balancer/scenario"
)

// Both Go and the browser use these exact cases, including raw ambiguous JSON tokens.
func TestScenarioSharedValidationCases(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "examples", "scenarios", "validation", "cases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Base  string
		Cases []struct {
			Name       string
			Valid      bool
			ErrorField string
			Changes    []struct {
				Path  []string
				Value any
			}
			Replacements []struct{ From, To string }
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("the shared validation corpus is empty")
	}
	base, err := os.ReadFile(filepath.Join(filepath.Dir(path), corpus.Base))
	if err != nil {
		t.Fatal(err)
	}

	for _, example := range corpus.Cases {
		t.Run(example.Name, func(t *testing.T) {
			// Start fresh: one case must never change the next case's input.
			var input map[string]any
			if err := json.Unmarshal(base, &input); err != nil {
				t.Fatal(err)
			}
			for _, change := range example.Changes {
				setValidationFixtureValue(t, input, change.Path, change.Value)
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			document := string(encoded)

			// Text replacement preserves tokens that JSON parse/stringify would normalize.
			for _, replacement := range example.Replacements {
				if !strings.Contains(document, replacement.From) {
					t.Fatalf("fixture replacement did not match %q", replacement.From)
				}
				document = strings.Replace(document, replacement.From, replacement.To, 1)
			}

			decoded, err := scenario.DecodeDocument([]byte(document))
			if err == nil {
				_, err = scenario.Normalize(decoded)
			}
			if example.Valid {
				if err != nil {
					t.Fatalf("valid shared case was rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid shared case was accepted")
			}
			if !strings.Contains(err.Error(), example.ErrorField) {
				t.Fatalf("want an error for %q, got %v", example.ErrorField, err)
			}
		})
	}
}

// A fixture path such as [states, 0, resources, backends, 0, port] edits one field.
func setValidationFixtureValue(t *testing.T, root any, path []string, value any) {
	t.Helper()
	if len(path) == 0 {
		t.Fatal("fixture change has an empty path")
	}

	parent := root
	for index, key := range path {
		last := index == len(path)-1
		switch node := parent.(type) {
		case map[string]any:
			if last {
				node[key] = value
				return
			}
			parent = node[key]
		case []any:
			position, err := strconv.Atoi(key)
			if err != nil || position < 0 || position >= len(node) {
				t.Fatalf("invalid fixture array index %q", key)
			}
			if last {
				node[position] = value
				return
			}
			parent = node[position]
		default:
			t.Fatalf("fixture path %v does not reach an object or array", path)
		}
	}
}
