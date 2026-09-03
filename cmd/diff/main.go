package main

import (
	"context"
	"fmt"
	"os"

	"driftsync/adapters/specfile"
	"driftsync/canonicalize"
	"driftsync/diff"
	"driftsync/ir"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: diff <published-spec> <code-spec>")
		os.Exit(2)
	}

	published, err := loadCanonical(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load published:", err)
		os.Exit(1)
	}
	code, err := loadCanonical(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load code:", err)
		os.Exit(1)
	}

	report := diff.Diff(published, code)

	if len(report.Changes) == 0 {
		fmt.Println("no drift: published spec matches code")
		return
	}

	fmt.Printf("%d change(s), %d breaking:\n\n", len(report.Changes), report.Breaking())
	for _, c := range report.Changes {
		line := fmt.Sprintf("  [%-8s] %-26s %s", c.Severity, c.Kind, c.Location)
		if c.From != "" || c.To != "" {
			line += fmt.Sprintf("  (%s -> %s)", c.From, c.To)
		}
		if c.Note != "" {
			line += "  [" + c.Note + "]"
		}
		fmt.Println(line)
	}

	// CI gate: non-zero exit when breaking drift exists.
	if report.Breaking() > 0 {
		os.Exit(1)
	}
}

// loadCanonical: file -> IR -> canonical 3.1 IR.
func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}
