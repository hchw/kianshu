package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github/hchw/kianshu/internal/openai"
)

type runtimeProvider struct {
	streamed bool
}

func (runtimeProvider) ChatCompletion(context.Context, openai.Provider, openai.CompletionRequest) (*openai.CompletionResponse, error) {
	return &openai.CompletionResponse{}, nil
}

func (p *runtimeProvider) StreamChatCompletion(_ context.Context, _ openai.Provider, _ openai.CompletionRequest, onText openai.StreamCallback, onReasoning openai.ReasoningCallback) (*openai.CompletionResponse, error) {
	p.streamed = true
	onReasoning("思考")
	onText("回答")
	return &openai.CompletionResponse{}, nil
}

func (runtimeProvider) GetBaseURL() string     { return "" }
func (runtimeProvider) GetAPIKey() string      { return "" }
func (runtimeProvider) GetModel() string       { return "test" }
func (runtimeProvider) GetStrictContent() bool { return false }

func TestEventKeepsExistingWireShape(t *testing.T) {
	data, err := json.Marshal(Event{Kind: EventKindText, Round: 2, Text: "回答"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"kind":"text","round":2,"text":"回答"}` {
		t.Fatalf("unexpected event JSON: %s", data)
	}
}

func TestCompactMessagesKeepsSystemSummaryAndLastUser(t *testing.T) {
	system, user, oldUser := "system", "new", "old"
	msgs := []openai.Message{
		{Role: "system", Content: &system},
		{Role: "user", Content: &oldUser},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{Function: openai.FunctionCall{Name: "create_case_node"}}}},
		{Role: "user", Content: &user},
	}
	got := CompactMessages(msgs, func(ops []string) string { return ops[0] })
	if len(got) != 3 || got[0].Role != "system" || got[1].Content == nil || *got[1].Content != "create_case_node" || *got[2].Content != "new" {
		t.Fatalf("unexpected compacted messages: %#v", got)
	}
}

func TestCompleteWithRetryStopsOnSuccess(t *testing.T) {
	client := &retryClient{}
	_, err := CompleteWithRetry(context.Background(), client, client, openai.CompletionRequest{}, nil, nil, 2)
	if err != nil || client.calls != 2 {
		t.Fatalf("retry result: err=%v calls=%d", err, client.calls)
	}
}

type retryClient struct{ calls int }

func (c *retryClient) ChatCompletion(context.Context, openai.Provider, openai.CompletionRequest) (*openai.CompletionResponse, error) {
	c.calls++
	if c.calls == 1 {
		return nil, context.DeadlineExceeded
	}
	return &openai.CompletionResponse{}, nil
}
func (c *retryClient) GetBaseURL() string     { return "" }
func (c *retryClient) GetAPIKey() string      { return "" }
func (c *retryClient) GetModel() string       { return "test" }
func (c *retryClient) GetStrictContent() bool { return false }

func TestCompleteUsesStreamingCapabilityWithoutChangingRequestPath(t *testing.T) {
	client := &runtimeProvider{}
	var reasoning, text string
	_, err := Complete(context.Background(), client, client, openai.CompletionRequest{Model: "test"}, func(s string) { text += s }, func(s string) { reasoning += s })
	if err != nil {
		t.Fatal(err)
	}
	if !client.streamed || text != "回答" || reasoning != "思考" {
		t.Fatalf("unexpected runtime result: streamed=%v text=%q reasoning=%q", client.streamed, text, reasoning)
	}
}

func TestCompleteFallsBackToNonStreamingClient(t *testing.T) {
	client := runtimeNonStreaming{}
	if _, err := Complete(context.Background(), client, client, openai.CompletionRequest{}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

type runtimeNonStreaming struct{}

func (runtimeNonStreaming) ChatCompletion(context.Context, openai.Provider, openai.CompletionRequest) (*openai.CompletionResponse, error) {
	return &openai.CompletionResponse{}, nil
}
func (runtimeNonStreaming) GetBaseURL() string     { return "" }
func (runtimeNonStreaming) GetAPIKey() string      { return "" }
func (runtimeNonStreaming) GetModel() string       { return "test" }
func (runtimeNonStreaming) GetStrictContent() bool { return false }
