package enrich

import (
	"context"
	"fmt"

	"driftsync/llm"
)

// LLMEnricher adapts any llm.Client into an Enricher. The DOMAIN knowledge —
// how to prompt for an OpenAPI description — lives here; the transport lives in
// the llm package. Swapping providers/fallback happens by passing a different
// llm.Client (e.g. llm.NewFallback(anthropic, openai)).
type LLMEnricher struct {
	Client llm.Client
}

func NewLLMEnricher(c llm.Client) *LLMEnricher { return &LLMEnricher{Client: c} }

func (l *LLMEnricher) Name() string { return l.Client.Name() }

func (l *LLMEnricher) Describe(ctx context.Context, req DescribeRequest) (string, error) {
	return l.Client.Complete(ctx, llm.Request{
		MaxTokens: 120,
		System: "You write terse, factual one-line OpenAPI field descriptions. " +
			"Reply with the description text only — no quotes, no prefix, no trailing period.",
		Prompt: fmt.Sprintf(
			"Write a one-line description for a new %s named %q in schema %q.\n"+
				"Its OpenAPI fragment is:\n%s",
			req.Kind, req.Name, req.Schema, req.Snippet),
	})
}
