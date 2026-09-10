package main

import (
	"fmt"

	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/patch"
)

func cmdPatch(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: driftsync patch <published-spec> <code-spec>")
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
	patches, err := patch.FromReport(report, code)
	if err != nil {
		return fmt.Errorf("patch: %w", err)
	}

	if len(patches) == 0 {
		fmt.Println("no patches proposed")
		return nil
	}
	fmt.Printf("%d patch(es) proposed (review before applying):\n\n", len(patches))
	for _, p := range patches {
		fmt.Printf("  [%-8s] %-7s %s\n", p.Severity, p.Op, p.Path)
		if p.Value != nil {
			fmt.Printf("            value: %s\n", inlineYAML(p.Value))
		}
		if len(p.Match) > 0 {
			fmt.Printf("            match: %v\n", p.Match)
		}
		fmt.Printf("            reason: %s\n", p.Reason)
	}
	return nil
}
