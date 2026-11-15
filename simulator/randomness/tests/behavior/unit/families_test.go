package randomness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"load-balancer/randomness"
)

// Every activated family shares its raw acceptance cases with the frontend.
func TestFamilySharedProfileCases(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "..", "..", "examples", "randomness", "families")
	files, err := filepath.Glob(filepath.Join(directory, "*-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("declared distribution families need shared profile cases")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var corpus struct {
			Family string `json:"family"`
			Cases  []struct {
				Name     string `json:"name"`
				Valid    bool   `json:"valid"`
				Document string `json:"document"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(data, &corpus); err != nil {
			t.Fatal(err)
		}
		if len(corpus.Cases) == 0 {
			t.Fatalf("%s contains no cases", file)
		}
		for _, example := range corpus.Cases {
			t.Run(corpus.Family+"/"+example.Name, func(t *testing.T) {
				definition, err := randomness.DecodeProfile([]byte(example.Document))
				if !example.Valid {
					if err == nil {
						t.Fatal("invalid shared profile was accepted")
					}
					if !reflect.DeepEqual(definition, randomness.Profile{}) {
						t.Fatal("failed decode retained partial data")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := randomness.EncodeProfile(definition)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := randomness.DecodeProfile(encoded)
				if err != nil || !reflect.DeepEqual(restored, definition) {
					t.Fatalf("profile roundtrip changed: %v", err)
				}
			})
		}
	}
}
