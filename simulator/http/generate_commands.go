//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
)

func main() {
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}

func generate() error {
	source, err := parser.ParseFile(token.NewFileSet(), "../internal/contract/commands.go", nil, 0)
	if err != nil {
		return err
	}
	var output bytes.Buffer
	output.WriteString("// Code generated from simulator/http/commands.go; DO NOT EDIT.\n")
	output.WriteString("// Regenerate: cd simulator && go generate ./http\n\n")
	output.WriteString("export enum CommandType {\n")
	count := 0
	for _, declaration := range source.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, specification := range group.Specs {
			value := specification.(*ast.ValueSpec)
			commandType, ok := value.Type.(*ast.Ident)
			if !ok || commandType.Name != "CommandType" || len(value.Names) != 1 || len(value.Values) != 1 {
				return fmt.Errorf("expected a named CommandType string constant")
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return fmt.Errorf("%s must have a string literal value", value.Names[0].Name)
			}
			fmt.Fprintf(&output, "  %s = %s,\n", value.Names[0].Name, literal.Value)
			count++
		}
	}
	if count == 0 {
		return fmt.Errorf("no CommandType constants found")
	}
	output.WriteString("}\n")
	return os.WriteFile("../../visualizer/src/commands.ts", output.Bytes(), 0644)
}
