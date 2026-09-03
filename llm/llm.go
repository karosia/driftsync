package llm

import (
	"context"
	"fmt"
)

// Request is a provider-neutral completion request. No vendor types.
type Request struct {
	System    string
	Prompt    string
	MaxTokens int
}

// Client is one text-in/text-out LLM provider.
type Client interface {
	Complete(ctx context.Context, req Request) (string, error)
	Name() string
}

// Fallback tries clients in order, returning the first success. It's itself a
// Client, so callers can't tell a single provider from a chain. Every provider's
// error is aggregated so a total failure explains what each one said.
type Fallback struct {
	Clients []Client
}

func NewFallback(clients ...Client) *Fallback { return &Fallback{Clients: clients} }

func (f *Fallback) Name() string {
	names := make([]string, 0, len(f.Clients))
	for _, c := range f.Clients {
		names = append(names, c.Name())
	}
	return "fallback(" + join(names, ",") + ")"
}

func (f *Fallback) Complete(ctx context.Context, req Request) (string, error) {
	if len(f.Clients) == 0 {
		return "", fmt.Errorf("llm: no providers configured")
	}
	var errs []string
	for _, c := range f.Clients {
		out, err := c.Complete(ctx, req)
		if err == nil {
			return out, nil
		}
		errs = append(errs, c.Name()+": "+err.Error())
	}
	return "", fmt.Errorf("llm: all providers failed: %s", join(errs, "; "))
}

func join(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
