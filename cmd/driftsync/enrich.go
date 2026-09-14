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
	c, ok := buildLLMClient()
	if !ok {
		return enrich.StubEnricher{}
	}
	return enrich.NewLLMEnricher(c)
}

// buildLLMClient is the same provider selection as buildEnricher, for
// features that need a bare completion rather than a diff-description
// Enricher (e.g. init --describe). ok is false with no keys configured —
// unlike buildEnricher, there's no stub fallback: a feature that needs a real
// answer from a model has nothing useful to fall back to.
func buildLLMClient() (llm.Client, bool) {
	var clients []llm.Client
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		clients = append(clients, llm.NewAnthropic(k, envOr("ANTHROPIC_MODEL", "claude-sonnet-4-5")))
	}
	if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		clients = append(clients, llm.NewOpenAI(k, envOr("OPENAI_MODEL", "gpt-4o-mini")))
	}
	switch len(clients) {
	case 0:
		return nil, false
	case 1:
		return clients[0], true
	default:
		return llm.NewFallback(clients...), true
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
