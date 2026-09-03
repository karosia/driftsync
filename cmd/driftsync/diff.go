// cmd/driftsync/diff.go
package main

import (
	"fmt"
	"os"

	"driftsync/diff"
)

func cmdDiff(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: driftsync diff <published-spec> <code-spec>")
	}
	published, err := loadCanonical(args[0])
	if err != nil {
		return fmt.Errorf("load published: %w", err)
	}
	code, err := loadCanonical(args[1])
	if err != nil {
		return fmt.Errorf("load code: %w", err)
	}

	report := diff.Diff(published, code)
	if len(report.Changes) == 0 {
		fmt.Println("no drift: published spec matches code")
		return nil
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

	// CI gate: breaking drift -> exit 1. Handled here (not via returned error)
	// so the message above already printed and the code is deterministic.
	if report.Breaking() > 0 {
		os.Exit(1)
	}
	return nil
}
