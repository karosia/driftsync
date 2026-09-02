// cmd/genspec/main.go
package main

import (
	"context"
	"fmt"
	"os"

	"driftsync/adapters/humaadapter"
	"driftsync/ir"
	"driftsync/sampleapi"
)

// outputPath is where the code-side contract is emitted.
// In a real target repo this would be committed or uploaded as a CI artifact,
// then read back by the (separate) diff CLI — the "file boundary" of method (1).
const outputPath = "openapi.gen.yaml"

func main() {
	// 1) Build the extraction target. In a real target repo this line would be
	//    the app's own API constructor (e.g. myapp.NewAPI()).
	api := sampleapi.New()

	// 2) Extract its contract via the adapter, held as the ir.Adapter interface —
	//    the consuming code depends on the interface, not on humaadapter directly.
	var adapter ir.Adapter = humaadapter.New(api)
	doc, err := adapter.Extract(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract failed:", err)
		os.Exit(1)
	}

	// 3) Emit the spec to disk. This file is the ONLY thing that crosses the
	//    boundary — no diff engine or agent dependencies leak into the target.
	if err := os.WriteFile(outputPath, doc.Raw, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		os.Exit(1)
	}

	// 4) Smoke summary: confirm version + walk operations so we can eyeball that
	//    the IR is traversable (this stays useful during development).
	fmt.Printf("wrote %s (OpenAPI %s)\n", outputPath, doc.Version)
	if doc.Model.Paths == nil {
		return
	}
	// libopenapi uses ordered maps; iterate with First()/Next()/Key()/Value().
	for p := doc.Model.Paths.PathItems.First(); p != nil; p = p.Next() {
		path := p.Key()
		item := p.Value()
		for op := item.GetOperations().First(); op != nil; op = op.Next() {
			fmt.Printf("  %-6s %s  (opId=%s)\n",
				op.Key(), path, op.Value().OperationId)
		}
	}
}
