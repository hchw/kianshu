package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type probeProvider struct{ base string }

func (p probeProvider) GetBaseURL() string     { return p.base }
func (p probeProvider) GetAPIKey() string      { return "" }
func (p probeProvider) GetModel() string       { return "m" }
func (p probeProvider) GetStrictContent() bool { return false }

func msg(s string) *string { return &s }

func TestReasoningProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"let me think \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"deeply\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"final\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New()
	prov := probeProvider{base: srv.URL}
	var reasoning, content []string
	var mu sync.Mutex
	req := CompletionRequest{Model: "m", Messages: []Message{{Role: "user", Content: msg("hi")}}}

	_, err := c.StreamChatCompletion(context.Background(), prov, req,
		func(t string) { mu.Lock(); defer mu.Unlock(); content = append(content, t) },
		func(t string) { mu.Lock(); defer mu.Unlock(); reasoning = append(reasoning, t) })
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(reasoning) != 2 || reasoning[0]+reasoning[1] != "let me think deeply" {
		t.Fatalf("reasoning mismatch: %v", reasoning)
	}
	if len(content) != 1 || content[0] != "final" {
		t.Fatalf("content mismatch: %v", content)
	}
}
