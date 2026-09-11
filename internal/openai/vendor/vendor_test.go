package vendor

import (
	"net/http"
	"testing"
)

func TestMatchOpenCodeByURL(t *testing.T) {
	for _, baseURL := range []string{
		"https://opencode.ai/v1",
		"https://api.OPENCODE.ai",
		"http://localhost:8080/opencode",
	} {
		if v := Match(baseURL); v == nil || v.Name != "opencode" {
			t.Fatalf("Match(%q) = %#v, want opencode", baseURL, v)
		}
	}
	for _, baseURL := range []string{"https://api.openai.com/v1", "http://localhost:11434/v1", ""} {
		if v := Match(baseURL); v != nil {
			t.Fatalf("Match(%q) = %#v, want nil", baseURL, v)
		}
	}
}

func TestApplyHeadersOpenCode(t *testing.T) {
	h := http.Header{}
	ApplyHeaders(h, "https://opencode.ai/v1", "kianshu-flow-1-session-2")
	if got := h.Get("User-Agent"); got != CodingAgentUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, CodingAgentUserAgent)
	}
	if got := h.Get("x-opencode-session"); got != "kianshu-flow-1-session-2" {
		t.Fatalf("x-opencode-session = %q", got)
	}
}

func TestApplyHeadersOpenCodeWithoutSession(t *testing.T) {
	h := http.Header{}
	ApplyHeaders(h, "https://opencode.ai/v1", "")
	if h.Get("User-Agent") == "" {
		t.Fatal("opencode without session should still set User-Agent")
	}
	if got := h.Get("x-opencode-session"); got != "" {
		t.Fatalf("x-opencode-session = %q, want empty", got)
	}
}

func TestApplyHeadersOtherVendorKeepsDefault(t *testing.T) {
	h := http.Header{}
	ApplyHeaders(h, "https://api.openai.com/v1", "session-x")
	if h.Get("User-Agent") != "" || h.Get("x-opencode-session") != "" {
		t.Fatalf("unexpected headers for non-opencode provider: %v", h)
	}
}
