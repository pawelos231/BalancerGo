package main

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
		"model", "scenario", "internal/simulation",
	}
	type rule struct {
		directory string
		forbidden []string
	}
	rules := make([]rule, 0, len(domain)+2)
	for _, directory := range domain {
		rules = append(rules, rule{directory, []string{"internal/contract", "internal/presentation", "http"}})
	}
	rules = append(rules,
		// An empty package prefix forbids every import from this module: contract
		// DTOs cannot acquire domain, presentation, transport or application types.
		rule{"internal/contract", []string{""}},
		rule{"http", []string{"internal/simulation", "internal/presentation", "model", "network"}},
	)

	const module = "load-balancer"
	for _, restriction := range rules {
		t.Run(restriction.directory, func(t *testing.T) {
			files := 0
			err := filepath.WalkDir(filepath.FromSlash(restriction.directory), func(path string, entry fs.DirEntry, walkErr error) error {
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
