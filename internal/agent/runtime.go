// Package agent contains infrastructure shared by LLM agents.
package agent

import (
	"context"
	"strings"
	"time"

	"github/hchw/kianshu/internal/openai"
)

// EventKind identifies a common Agent Runtime event.
const (
	EventKindRound      = "round"
	EventKindText       = "text"
	EventKindReasoning  = "reasoning"
	EventKindTool       = "tool"
	EventKindCheckpoint = "checkpoint"
)

// Event is the transport-neutral event shared by Agent implementations.
type Event struct {
	Kind   string `json:"kind,omitempty"`
	Round  int    `json:"round"`
	Tool   string `json:"tool,omitempty"`
	Args   any    `json:"args,omitempty"`
	Result any    `json:"result,omitempty"`
	Text   string `json:"text,omitempty"`
}

// EmitFunc receives one real-time Agent Runtime event.
type EmitFunc func(Event)

// FixMessageHistory removes an incomplete trailing assistant tool-call round.
// It keeps valid history usable after an interrupted Agent request.
func FixMessageHistory(msgs []openai.Message) []openai.Message {
	last := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && len(msgs[i].ToolCalls) > 0 {
			last = i
			break
		}
	}
	if last < 0 {
		return msgs
	}
	expected := map[string]bool{}
	for _, call := range msgs[last].ToolCalls {
		expected[call.ID] = true
	}
	seen := map[string]bool{}
	for _, msg := range msgs[last+1:] {
		if msg.Role == "tool" {
			seen[msg.ToolCallID] = true
		}
	}
	for id := range expected {
		if !seen[id] {
			return msgs[:last]
		}
	}
	return msgs
}

// ChatClient is the non-streaming capability required by the common agent
// runtime. The service layer supplies the existing ChatProvider implementation.
type ChatClient interface {
	ChatCompletion(context.Context, openai.Provider, openai.CompletionRequest) (*openai.CompletionResponse, error)
}

// StreamingChatClient optionally provides token and reasoning streaming.
type StreamingChatClient interface {
	ChatClient
	StreamChatCompletion(context.Context, openai.Provider, openai.CompletionRequest, openai.StreamCallback, openai.ReasoningCallback) (*openai.CompletionResponse, error)
}

// ThinkingUnset means that the request omits the thinking field.
const ThinkingUnset = "unset"

// ThinkingMode normalizes a user-supplied reasoning mode.
func ThinkingMode(value string) string {
	if strings.TrimSpace(value) == "" {
		return "disabled"
	}
	return value
}

// CompletionRequest builds a provider request without flow-specific semantics.
func CompletionRequest(provider openai.Provider, messages []openai.Message, thinking string) openai.CompletionRequest {
	req := openai.CompletionRequest{Model: provider.GetModel(), Messages: messages}
	if normalized := ThinkingMode(thinking); normalized != ThinkingUnset {
		req.Thinking = &openai.ThinkingConfig{Type: normalized}
	}
	if provider.GetStrictContent() {
		req.ForceContentString = true
	}
	return req
}

// CompactMessages keeps the system prompt, a caller-provided tool summary,
// and the last user message. It is independent of persistence and target flow.
func CompactMessages(msgs []openai.Message, summarize func([]string) string) []openai.Message {
	if len(msgs) <= 2 {
		return msgs
	}
	var operations []string
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			for _, call := range msg.ToolCalls {
				operations = append(operations, call.Function.Name)
			}
		}
	}
	compressed := []openai.Message{}
	if len(msgs) > 0 && msgs[0].Role == "system" {
		compressed = append(compressed, msgs[0])
	}
	if len(operations) > 0 && summarize != nil {
		if summary := summarize(operations); summary != "" {
			compressed = append(compressed, openai.Message{Role: "assistant", Content: &summary})
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			compressed = append(compressed, msgs[i])
			break
		}
	}
	return compressed
}

// CompleteWithRetry performs one completion with bounded transient retries.
// Cancellation is checked between attempts and is never retried.
func CompleteWithRetry(ctx context.Context, client ChatClient, provider openai.Provider, req openai.CompletionRequest, onText openai.StreamCallback, onReasoning openai.ReasoningCallback, attempts int) (*openai.CompletionResponse, error) {
	if attempts < 1 {
		attempts = 1
	}
	var response *openai.CompletionResponse
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		response, err = Complete(ctx, client, provider, req, onText, onReasoning)
		if err == nil {
			return response, nil
		}
		if ctx.Err() != nil || attempt+1 >= attempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return nil, err
}

// Complete prefers streaming when the configured client supports it and falls
// back to the existing non-streaming completion path otherwise. It intentionally
// contains no flow or Case Flow semantics.
func Complete(
	ctx context.Context,
	client ChatClient,
	provider openai.Provider,
	req openai.CompletionRequest,
	onText openai.StreamCallback,
	onReasoning openai.ReasoningCallback,
) (*openai.CompletionResponse, error) {
	if streaming, ok := client.(StreamingChatClient); ok {
		return streaming.StreamChatCompletion(ctx, provider, req, onText, onReasoning)
	}
	return client.ChatCompletion(ctx, provider, req)
}
