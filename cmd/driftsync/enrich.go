package main

import (
	"context"
	"fmt"
	"os"

	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/enrich"
	"github.com/karosia/driftsync/llm"
	"github.com/karosia/driftsync/patch"
)

func cmdEnrich(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: driftsync enrich <published-spec> <code-spec>")
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
	return nil
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
		return enrich.StubEnricher{}
	case 1:
		return enrich.NewLLMEnricher(clients[0])
	default:
		return enrich.NewLLMEnricher(llm.NewFallback(clients...))
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
