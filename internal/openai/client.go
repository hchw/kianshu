package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is an assistant tool invocation.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a function tool exposed to the model.
type Tool struct {
	Type     string         `json:"type"`
	Function ToolFunction   `json:"function"`
}

// ToolFunction describes a tool's name, description, and JSON schema.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// CompletionRequest is an OpenAI-compatible chat completion request.
type CompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// CompletionResponse is the subset of the chat completion response we need.
type CompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Provider is the minimal provider config the client needs.
type Provider interface {
	GetBaseURL() string
	GetAPIKey() string
	GetModel() string
}

// Client calls an OpenAI-compatible endpoint.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a sane timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// ChatCompletion sends a chat completion request to the provider.
func (c *Client) ChatCompletion(ctx context.Context, p Provider, req CompletionRequest) (*CompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.GetBaseURL(), "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if k := p.GetAPIKey(); k != "" {
		httpReq.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provider 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out CompletionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("provider 响应解析失败: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("provider error: %s", out.Error.Message)
	}
	return &out, nil
}

// Test verifies provider connectivity with a minimal chat completion request.
func (c *Client) Test(ctx context.Context, p Provider) error {
	msg := "ping"
	req := CompletionRequest{
		Model: p.GetModel(),
		Messages: []Message{{Role: "user", Content: &msg}},
	}
	if _, err := c.ChatCompletion(ctx, p, req); err != nil {
		return err
	}
	return nil
}
