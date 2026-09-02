package main

import (
	"context"
	"fmt"
	"os"

	"driftsync/adapters/specfile"
	"driftsync/ir"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loadspec <path-to-openapi-spec>")
		os.Exit(2)
	}
	path := os.Args[1]

	// Held as ir.Adapter — identical consumption to the code side, proving both
	// sources converge on one contract.
	var adapter ir.Adapter = specfile.New(path)
	doc, err := adapter.Extract(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "load failed:", err)
		os.Exit(1)
	}

	fmt.Printf("loaded %s (OpenAPI %s)\n", path, doc.Version)
	if doc.Model.Paths == nil {
		return
	}
	for p := doc.Model.Paths.PathItems.First(); p != nil; p = p.Next() {
		path := p.Key()
		item := p.Value()
		for op := item.GetOperations().First(); op != nil; op = op.Next() {
			fmt.Printf("  %-6s %s  (opId=%s)\n",
				op.Key(), path, op.Value().OperationId)
		}
	}
}
