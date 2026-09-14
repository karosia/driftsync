package enrich

import (
	"context"
	"fmt"
	"strings"

	"github.com/karosia/driftsync/llm"
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

func (l *LLMEnricher) SuggestRename(ctx context.Context, req RenameCandidate) (bool, string, error) {
	out, err := l.Client.Complete(ctx, llm.Request{
		MaxTokens: 60,
		System: "You judge whether a removed OpenAPI field and an added OpenAPI field " +
			"in the same schema are actually the same field renamed, not two unrelated " +
			"fields. Reply with exactly two lines: the first is YES or NO, the second is " +
			"one short reason.",
		Prompt: fmt.Sprintf(
			"Schema %q, %s: field %q (%s) was removed and field %q (%s) was added. "+
				"Is %q likely %q renamed?",
			req.Schema, req.Direction, req.FromName, req.TypeSig, req.ToName, req.TypeSig,
			req.ToName, req.FromName),
	})
	if err != nil {
		return false, "", err
	}
	lines := strings.SplitN(strings.TrimSpace(out), "\n", 2)
	ok := len(lines) > 0 && strings.EqualFold(strings.TrimSpace(lines[0]), "YES")
	note := ""
	if len(lines) > 1 {
		note = strings.TrimSpace(lines[1])
	}
	return ok, note, nil
}
