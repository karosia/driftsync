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

type OpenAI struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewOpenAI(apiKey, model string) *OpenAI {
	return &OpenAI{APIKey: apiKey, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (o *OpenAI) Name() string { return "openai:" + o.Model }

type openaiReq struct {
	Model string `json:"model"`
	// max_tokens is deprecated and rejected by reasoning models; this one
	// works on every current chat model and also caps reasoning tokens.
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	Messages            []openaiMessage `json:"messages"`
}
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type openaiResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (string, error) {
	if o.APIKey == "" {
		return "", fmt.Errorf("missing API key")
	}
	// OpenAI carries the system prompt as a message with role "system".
	msgs := []openaiMessage{}
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, openaiMessage{Role: "user", Content: req.Prompt})

	oreq := openaiReq{Model: o.Model, MaxCompletionTokens: req.MaxTokens, Messages: msgs}
	// gpt-6-luna reasons at "medium" by default, which would eat our 60-150
	// token budgets before any reply. Non-reasoning models reject the field.
	if o.Model == "gpt-6-luna" {
		oreq.ReasoningEffort = "none"
	}
	body, err := json.Marshal(oreq)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey) // Bearer, unlike Anthropic
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var parsed openaiResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("api error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response")
	}
	return parsed.Choices[0].Message.Content, nil
}
