package main

import (
	"context"
	"fmt"
	"os"

	"driftsync/adapters/specfile"
	"driftsync/canonicalize"
	"driftsync/diff"
	"driftsync/enrich"
	"driftsync/ir"
	"driftsync/llm"
	"driftsync/patch"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: enrich <published-spec> <code-spec>")
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

	e := buildEnricher()
	res := enrich.Apply(context.Background(), e, patches)
	fmt.Fprintf(os.Stderr, "enricher=%s  enriched=%d skipped=%d failed=%d\n\n",
		e.Name(), res.Enriched, res.Skipped, res.Failed)

	for _, p := range patches {
		line := fmt.Sprintf("  [%-8s] %-7s %s", p.Severity, p.Op, p.Path)
		if p.Description != "" {
			line += "\n            description: " + p.Description
		}
		fmt.Println(line)
	}
}

// buildEnricher assembles providers by available keys. Multiple keys -> a
// fallback chain (Anthropic first, OpenAI next). No keys -> the offline stub.
func buildEnricher() enrich.Enricher {
	var clients []llm.Client
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		clients = append(clients, llm.NewAnthropic(k, envOr("ANTHROPIC_MODEL", "claude-sonnet-4-5")))
	}
	if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		clients = append(clients, llm.NewOpenAI(k, envOr("OPENAI_MODEL", "gpt-4o-mini")))
	}
	switch len(clients) {
	case 0:
		return enrich.StubEnricher{} // offline: pipeline still runs end-to-end
	case 1:
		return enrich.NewLLMEnricher(clients[0])
	default:
		return enrich.NewLLMEnricher(llm.NewFallback(clients...)) // Anthropic -> OpenAI
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}
