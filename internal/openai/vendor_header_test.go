package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProviderRequestSendsOpenCodeHeaders 验证:当 provider 地址指向 opencode 时,
// 客户端发送专属 User-Agent,并把 ctx 上的会话 ID 写入 x-opencode-session。
func TestProviderRequestSendsOpenCodeHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer srv.Close()

	// httptest 地址不含 opencode;用带路径的 URL 触发 opencode 适配。
	provider := probeProvider{base: srv.URL + "/opencode/v1"}
	ctx := WithSessionID(context.Background(), "kianshu-flow-7-session-3")
	req := CompletionRequest{Model: "m", Messages: []Message{{Role: "user", Content: msg("hi")}}}

	if _, err := New().ChatCompletion(ctx, provider, req); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if ua := got.Get("User-Agent"); ua == "" || ua == "Go-http-client/1.1" {
		t.Fatalf("User-Agent = %q, want coding-agent UA", ua)
	}
	if sid := got.Get("x-opencode-session"); sid != "kianshu-flow-7-session-3" {
		t.Fatalf("x-opencode-session = %q", sid)
	}
}

// TestProviderRequestOmitsOpenCodeHeadersForOtherVendors 验证非 opencode provider
// 不受影响:不覆盖 User-Agent,也不发送会话头。
func TestProviderRequestOmitsOpenCodeHeadersForOtherVendors(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer srv.Close()

	provider := probeProvider{base: srv.URL + "/v1"}
	ctx := WithSessionID(context.Background(), "kianshu-flow-7-session-3")
	req := CompletionRequest{Model: "m", Messages: []Message{{Role: "user", Content: msg("hi")}}}

	if _, err := New().ChatCompletion(ctx, provider, req); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if sid := got.Get("x-opencode-session"); sid != "" {
		t.Fatalf("x-opencode-session = %q, want empty", sid)
	}
	if ua := got.Get("User-Agent"); ua != "" && ua != "Go-http-client/1.1" {
		t.Fatalf("User-Agent = %q, want default/empty", ua)
	}
}
