package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Anthropic struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{APIKey: apiKey, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (a *Anthropic) Name() string { return "anthropic:" + a.Model }

type anthropicReq struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"` // REQUIRED
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Thinking  *anthropicThinking `json:"thinking,omitempty"`
}
type anthropicThinking struct {
	Type string `json:"type"`
}
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type anthropicResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (string, error) {
	if a.APIKey == "" {
		return "", fmt.Errorf("missing API key")
	}
	areq := anthropicReq{
		Model:     a.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  []anthropicMessage{{Role: "user", Content: req.Prompt}},
	}
	// Sonnet 5.5 thinks by default, and thinking counts toward max_tokens —
	// our budgets (60-150) are sized for the reply alone. "between_tools" is
	// its thinking-off setting; every other model rejects it with a 400.
	if a.Model == "claude-sonnet-5-5" {
		areq.Thinking = &anthropicThinking{Type: "between_tools"}
	}
	body, err := json.Marshal(areq)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", a.APIKey) // NOT Authorization: Bearer
	httpReq.Header.Set("anthropic-version", "2026-10-21")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var parsed anthropicResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("api error: %s", parsed.Error.Message)
	}
	if parsed.StopReason == "refusal" {
		return "", fmt.Errorf("model declined the request (stop_reason: refusal)")
	}
	var out string
	for _, b := range parsed.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	if out == "" {
		return "", fmt.Errorf("empty response")
	}
	return out, nil
}
