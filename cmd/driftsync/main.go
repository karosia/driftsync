package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/adapters/specfile"
	"github.com/karosia/driftsync/canonicalize"
	"github.com/karosia/driftsync/ir"
)

// command is one subcommand: a name, a one-line help, and a runner.
type command struct {
	name string
	help string
	run  func(args []string) error
}

// version is overwritten at release time via -ldflags "-X main.version=...".
var version = "dev"

func commands() []command {
	return []command{
		{"init", "scaffold a driftsync.yaml (and optional workflows)", cmdInit},
		{"check", "config-driven: extract per driftsync.yaml, detect drift, gate CI", cmdCheck},
		{"sync", "config-driven: extract, fix the published spec, write a report", cmdSync},
		{"genspec", "extract code -> OpenAPI 3.1 file (built-in demo API)", cmdGenspec},
		{"diff", "detect drift: <published> <code>", cmdDiff},
		{"patch", "propose patches: <published> <code>", cmdPatch},
		{"apply", "apply patches to published: <published> <code>", cmdApply},
		{"enrich", "propose patches with LLM descriptions: <published> <code>", cmdEnrich},
		{"version", "print the driftsync version", cmdVersion},
	}
}

func cmdVersion([]string) error {
	fmt.Println("driftsync", version)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	name, args := os.Args[1], os.Args[2:]
	for _, c := range commands() {
		if c.name == name {
			if err := c.run(args); err != nil {
				fmt.Fprintln(os.Stderr, name+":", err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", name)
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: driftsync <command> [args]\n\ncommands:")
	for _, c := range commands() {
		fmt.Fprintf(os.Stderr, "  %-9s %s\n", c.name, c.help)
	}
}

// loadCanonical is shared by several subcommands: file -> IR -> canonical 3.1.
func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}

func inlineYAML(n *yaml.Node) string {
	out, err := yaml.Marshal(n)
	if err != nil {
		return "<unrenderable>"
	}
	return strings.Join(strings.Fields(strings.TrimSpace(string(out))), " ")
}
