package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Check production imports directly, including files for other build targets.
// Tests may cross boundaries to compare the public contract with native values.
func TestArchitectureDependencyDirections(t *testing.T) {
	domain := []string{
		"engine", "network", "client", "server", "balancer", "algorithms",
		"model", "scenario", "protocols", "internal/simulation",
	}
	type rule struct {
		directory string
		forbidden []string
	}
	rules := make([]rule, 0, len(domain)+2)
	for _, directory := range domain {
		rules = append(rules, rule{directory, []string{
			"internal/contract", "internal/presentation", "http",
			"internal/app", "internal/live", "internal/execution", "internal/testsupport",
		}})
	}
	rules = append(rules,
		// Congestion algorithms receive scalar facts; they cannot access hosts or the engine.
		rule{"protocols/tcp/congestion_control", []string{
			"engine", "network", "client", "server", "balancer", "algorithms", "scenario", "internal/simulation",
			"protocols/tcp/sender", "protocols/tcp/receiver",
		}},
		// Each endpoint owns its state; neither reaches into the other endpoint.
		rule{"protocols/tcp/sender", []string{"protocols/tcp/receiver"}},
		rule{"protocols/tcp/receiver", []string{"protocols/tcp/sender"}},
		// Protocols receive workload and host callbacks, without importing their callers.
		rule{"protocols", []string{"client", "server", "balancer", "algorithms", "scenario", "internal/simulation"}},
		// An empty package prefix forbids every import from this module: contract
		// DTOs cannot acquire domain, presentation, transport or application types.
		rule{"internal/contract", []string{""}},
		rule{"http", []string{
			"internal/simulation", "internal/presentation", "model", "network",
			"internal/app", "internal/live", "internal/execution", "internal/testsupport",
		}},
		// Starting the application stays above interactive sessions and execution.
		rule{"internal/live", []string{"internal/app", "internal/testsupport"}},
		rule{"internal/execution", []string{
			"internal/app", "internal/live", "internal/presentation", "http", "internal/testsupport",
		}},
		rule{"internal/presentation", []string{
			"internal/app", "internal/live", "internal/execution", "http", "internal/testsupport",
		}},
		// Shared test fixtures must never become a production dependency.
		rule{"internal/app", []string{"internal/testsupport"}},
	)

	// Algorithm packages share contracts and window rules, not each other's implementations.
	const congestionRoot = "protocols/tcp/congestion_control/"
	congestionAlgorithms := []string{"reno", "newreno", "tahoe", "cubic", "none"}
	for _, algorithm := range congestionAlgorithms {
		forbidden := []string{congestionRoot + "factory"}
		for _, other := range congestionAlgorithms {
			if other != algorithm {
				forbidden = append(forbidden, congestionRoot+other)
			}
		}
		rules = append(rules, rule{congestionRoot + algorithm, forbidden})
	}

	const module = "load-balancer"
	// This test now lives three directories below the simulator source root.
	sourceRoot := filepath.Join("..", "..", "..")
	for _, restriction := range rules {
		t.Run(restriction.directory, func(t *testing.T) {
			files := 0
			directory := filepath.Join(sourceRoot, filepath.FromSlash(restriction.directory))
			err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
					return nil
				}
				files++
				positions := token.NewFileSet()
				source, err := parser.ParseFile(positions, path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, declaration := range source.Imports {
					imported, err := strconv.Unquote(declaration.Path.Value)
					if err != nil {
						return err
					}
					for _, forbidden := range restriction.forbidden {
						prefix := module
						if forbidden != "" {
							prefix += "/" + forbidden
						}
						if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
							t.Errorf("%s: %s must not depend on %s", positions.Position(declaration.Pos()), restriction.directory, imported)
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if files == 0 {
				t.Fatal("architecture check found no production Go files")
			}
		})
	}
}
