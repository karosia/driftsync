package main

import (
	"context"
	"fmt"
	"os"

	"github.com/karosia/driftsync/adapters/humaadapter"
	"github.com/karosia/driftsync/ir"
	"github.com/karosia/driftsync/sampleapi"
)

func cmdGenspec(args []string) error {
	outputPath := "openapi.gen.yaml"
	if len(args) >= 1 {
		outputPath = args[0]
	}

	api := sampleapi.New()
	var adapter ir.Adapter = humaadapter.New(api)
	doc, err := adapter.Extract(context.Background())
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := os.WriteFile(outputPath, doc.Raw, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s (OpenAPI %s)\n", outputPath, doc.Version)
	return nil
}
