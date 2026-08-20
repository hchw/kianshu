package openai

import (
	"bufio"
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

// ThinkingConfig toggles the model's reasoning mode. Type "disabled" turns
// off chain-of-thought (deep thinking), supported by DeepSeek/Qwen/Kimi
// compatible providers; other providers ignore the field.
type ThinkingConfig struct {
	Type string `json:"type"`
}

// CompletionRequest is an OpenAI-compatible chat completion request.
type CompletionRequest struct {
	Model    string          `json:"model"`
	Messages []Message       `json:"messages"`
	Tools    []Tool          `json:"tools,omitempty"`
	Thinking *ThinkingConfig `json:"thinking,omitempty"`
	// ForceContentString 让 client 把 content 为 null 的消息改为空串发出,
	// 而非省略该字段。OpenAI 官方接受 null;ollama/vLLM 等严格服务端拒绝 null。
	// 这是 client 端的指令,不会作为请求字段发给 provider。
	ForceContentString bool `json:"-"`
	// Stream asks the provider to send the response as an SSE delta stream
	// (handled by StreamChatCompletion; ignored by ChatCompletion).
	Stream bool `json:"stream,omitempty"`
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
	// GetStrictContent 报告该 provider 是否拒绝 null content(ollama/vLLM 等),
	// 需要 client 在发送时把 null content 替换为空串。
	GetStrictContent() bool
}

// Client calls an OpenAI-compatible endpoint.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a sane timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// applyContentFallback 返回消息副本:当 ForceContentString 开启时,把任何
// content 为 nil 的消息替换为空串。部分严格的 OpenAI 兼容服务端(ollama/vLLM/
// SGLang)拒绝 null content 而要求字符串;官方 OpenAI 容忍 null。默认关闭时
// 原样返回,保持对 OpenAI 的兼容。
func (r CompletionRequest) applyContentFallback() []Message {
	if !r.ForceContentString {
		return r.Messages
	}
	out := make([]Message, len(r.Messages))
	for i, m := range r.Messages {
		if m.Content == nil {
			s := ""
			m.Content = &s
		}
		out[i] = m
	}
	return out
}

// ChatCompletion sends a chat completion request to the provider.
func (c *Client) ChatCompletion(ctx context.Context, p Provider, req CompletionRequest) (*CompletionResponse, error) {
	req.Messages = req.applyContentFallback()
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

// StreamCallback receives each incremental content delta as it arrives.
type StreamCallback func(text string)

// streamChunk is the delta frame shape of an OpenAI-compatible SSE stream.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StreamChatCompletion sends a chat completion with stream=true and delivers
// each content delta to onChunk in real time. Tool-call deltas (id/name/
// arguments fragments) are accumulated across frames and returned assembled in
// the final message, so callers treat it exactly like ChatCompletion.
func (c *Client) StreamChatCompletion(ctx context.Context, p Provider, req CompletionRequest, onChunk StreamCallback) (*CompletionResponse, error) {
	req.Stream = true
	req.Messages = req.applyContentFallback()
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	scanner := bufio.NewScanner(resp.Body)
	// Tool-call argument JSON can exceed the default 64KB line cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var contentBuf strings.Builder
	var toolCalls []ToolCall
	var role string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Keep-alive or non-JSON frames are skipped.
			continue
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("provider error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Role != "" {
			role = d.Role
		}
		if d.Content != "" {
			contentBuf.WriteString(d.Content)
			if onChunk != nil {
				onChunk(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			for len(toolCalls) <= tc.Index {
				toolCalls = append(toolCalls, ToolCall{Type: "function"})
			}
			t := &toolCalls[tc.Index]
			if tc.ID != "" {
				t.ID = tc.ID
			}
			t.Function.Name += tc.Function.Name
			t.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取流式响应失败: %w", err)
	}

	if role == "" {
		role = "assistant"
	}
	msg := Message{Role: role, ToolCalls: toolCalls}
	if contentBuf.Len() > 0 {
		s := contentBuf.String()
		msg.Content = &s
	}
	return &CompletionResponse{Choices: []struct {
		Message Message `json:"message"`
	}{{Message: msg}}}, nil
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
