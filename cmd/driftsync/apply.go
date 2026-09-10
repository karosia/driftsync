// cmd/driftsync/apply.go
package main

import (
	"fmt"
	"os"

	"github.com/karosia/driftsync/applier"
	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/patch"
)

func cmdApply(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: driftsync apply <published-spec> <code-spec>")
	}
	publishedPath, codePath := args[0], args[1]

	publishedCanon, err := loadCanonical(publishedPath)
	if err != nil {
		return fmt.Errorf("load published: %w", err)
	}
	codeCanon, err := loadCanonical(codePath)
	if err != nil {
		return fmt.Errorf("load code: %w", err)
	}
	report := diff.Diff(publishedCanon, codeCanon)
	patches, err := patch.FromReport(report, codeCanon)
	if err != nil {
		return fmt.Errorf("patch: %w", err)
	}

	publishedRaw, err := os.ReadFile(publishedPath)
	if err != nil {
		return fmt.Errorf("read published: %w", err)
	}
	result, applied, failed, err := applier.Apply(publishedRaw, patches)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}

	fmt.Fprintf(os.Stderr, "applied %d/%d patch(es), %d failed\n",
		len(applied), len(patches), len(failed))
	for _, f := range failed {
		fmt.Fprintf(os.Stderr, "  FAILED %s %s: %s\n", f.Patch.Op, f.Patch.Path, f.Reason)
	}
	fmt.Print(string(result))
	return nil
}
