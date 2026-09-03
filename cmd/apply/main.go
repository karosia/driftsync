package main

import (
	"context"
	"fmt"
	"os"

	"driftsync/adapters/specfile"
	"driftsync/applier"
	"driftsync/canonicalize"
	"driftsync/diff"
	"driftsync/ir"
	"driftsync/patch"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: apply <published-spec> <code-spec>")
		os.Exit(2)
	}
	publishedPath, codePath := os.Args[1], os.Args[2]

	// Detect + propose on canonical forms...
	publishedCanon, err := loadCanonical(publishedPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load published:", err)
		os.Exit(1)
	}
	codeCanon, err := loadCanonical(codePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load code:", err)
		os.Exit(1)
	}
	report := diff.Diff(publishedCanon, codeCanon)
	patches, err := patch.FromReport(report, codeCanon)
	if err != nil {
		fmt.Fprintln(os.Stderr, "patch:", err)
		os.Exit(1)
	}

	// ...but apply to the ORIGINAL published bytes (preserve prose/formatting).
	publishedRaw, err := os.ReadFile(publishedPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read published:", err)
		os.Exit(1)
	}
	result, applied, failed, err := applier.Apply(publishedRaw, patches)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apply:", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "applied %d/%d patch(es), %d failed\n\n",
		len(applied), len(patches), len(failed))
	for _, f := range failed {
		fmt.Fprintf(os.Stderr, "  FAILED %s %s: %s\n", f.Patch.Op, f.Patch.Path, f.Reason)
	}

	// Edited document goes to stdout (redirect to a file, or pipe into a PR step).
	fmt.Print(string(result))
}

func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}
