package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"driftsync/adapters/specfile"
	"driftsync/canonicalize"
	"driftsync/diff"
	"driftsync/ir"
	"driftsync/patch"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: patch <published-spec> <code-spec>")
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
	patches, err := patch.FromReport(report, code)
	if err != nil {
		fmt.Fprintln(os.Stderr, "patch:", err)
		os.Exit(1)
	}

	if len(patches) == 0 {
		fmt.Println("no patches proposed")
		return
	}
	fmt.Printf("%d patch(es) proposed (review before applying):\n\n", len(patches))
	for _, p := range patches {
		fmt.Printf("  [%-8s] %-7s %s\n", p.Severity, p.Op, p.Path)
		if p.Value != nil {
			fmt.Printf("            value: %s\n", inlineYAML(p.Value))
		}
		fmt.Printf("            reason: %s\n", p.Reason)
	}
}

func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}

// inlineYAML renders a node compactly for one-line display.
func inlineYAML(n *yaml.Node) string {
	out, err := yaml.Marshal(n)
	if err != nil {
		return "<unrenderable>"
	}
	return strings.Join(strings.Fields(strings.TrimSpace(string(out))), " ")
}
