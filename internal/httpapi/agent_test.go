package httpapi_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github/hchw/kianshu/internal/config"
	"github/hchw/kianshu/internal/httpapi"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"
	"github/hchw/kianshu/internal/service"
)

// agentFakeProvider is the test-injected ChatProvider for agent endpoints.
type agentFakeProvider struct {
	script []agentRound
	model  string
}

type agentRound struct {
	content   *string
	toolCalls []openai.ToolCall
}

func (f *agentFakeProvider) GetBaseURL() string { return "http://fake" }
func (f *agentFakeProvider) GetAPIKey() string  { return "sk" }
func (f *agentFakeProvider) GetModel() string   { return f.model }

func (f *agentFakeProvider) ChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest) (*openai.CompletionResponse, error) {
	var r agentRound
	if len(f.script) > 0 {
		r = f.script[0]
		f.script = f.script[1:]
	}
	return &openai.CompletionResponse{
		Choices: []struct {
			Message openai.Message `json:"message"`
		}{{Message: openai.Message{Content: r.content, ToolCalls: r.toolCalls}}},
	}, nil
}

// agentSharedServer starts a server with an injected fake provider.
func agentSharedServer(t *testing.T, fake service.ChatProvider) *httptest.Server {
	t.Helper()
	gdb := newTestDB(t)
	srv, err := httpapi.New(gdb, &config.Config{
		EncKey:     []byte("dev-only-32byte-secret-key-00001"),
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.AgentLLM = fake
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

// agentFixture sets up a test set, a flow, and a provider for the owner.
func agentFixture(t *testing.T, c *client) (uint, uint, uint) {
	t.Helper()
	ownerID := c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"agent"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))
	_, prov := c.do("POST", "/api/providers",
		fmt.Sprintf(`{"name":"llm","base_url":"http://fake","api_key":"sk","model":"m","enabled":true}`), http.StatusCreated)
	providerID := uint(prov["id"].(float64))
	return ownerID, flowID, providerID
}

func TestAgentSubmitAndSession(t *testing.T) {
	fake := &agentFakeProvider{model: "m", script: []agentRound{
		{toolCalls: []openai.ToolCall{
			{ID: "c1", Type: "function", Function: openai.FunctionCall{
				Name: "create_node", Arguments: `{"id":"n2","type":"adapter","parent":"n1"}`,
			}},
		}},
		{content: strPtrA("完成")},
	}}
	c := &client{t: t, ts: agentSharedServer(t, fake)}
	_, flowID, providerID := agentFixture(t, c)

	body := fmt.Sprintf(`{"provider_id":%d,"instruction":"加一个 adapter","mode":"edit"}`, providerID)
	_, out := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID), body, http.StatusOK)
	if out["finished"] != true {
		t.Fatalf("expected finished, got %v", out)
	}
	events, ok := out["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("expected tool events, got %v", out)
	}

	// Draft updated by the agent.
	_, draft := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if !strings.Contains(draft["tree"].(string), `"type":"adapter"`) {
		t.Fatalf("draft should contain created adapter node, got %v", draft["tree"])
	}

	// Session history persisted.
	_, sess := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/agent/session", flowID), "", http.StatusOK)
	if sess["status"] != "active" {
		t.Fatalf("expected active session, got %v", sess["status"])
	}
	msgs, ok := sess["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected persisted messages, got %v", sess)
	}
}

func TestAgentRequiresProvider(t *testing.T) {
	c := &client{t: t, ts: agentSharedServer(t, &agentFakeProvider{model: "m"})}
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"agent"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// Missing provider_id -> 400.
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID),
		`{"instruction":"x"}`, http.StatusBadRequest)
	// Nonexistent provider -> 404.
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID),
		`{"provider_id":999,"instruction":"x"}`, http.StatusNotFound)
}

func TestAgentGeneratePausesAndResumes(t *testing.T) {
	// First: generation pauses on a conflict.
	fake := &agentFakeProvider{model: "m", script: []agentRound{
		{content: strPtrA("Q: 认证方式冲突,请确认")},
	}}
	c := &client{t: t, ts: agentSharedServer(t, fake)}
	_, flowID, providerID := agentFixture(t, c)

	body := fmt.Sprintf(`{"provider_id":%d,"instruction":"生成登录","mode":"generate"}`, providerID)
	_, out := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID), body, http.StatusOK)
	if out["finished"] == true {
		t.Fatalf("expected paused generation, got %v", out)
	}
	events := out["events"].([]any)
	ev := events[0].(map[string]any)
	result := ev["result"].(map[string]any)
	if result["paused"] != true {
		t.Fatalf("expected paused flag, got %v", result)
	}
	if _, ok := result["questions"].([]any); !ok {
		t.Fatalf("expected questions list, got %v", result)
	}

	// Session shows paused with pending questions.
	_, sess := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/agent/session", flowID), "", http.StatusOK)
	if sess["status"] != "paused" {
		t.Fatalf("expected paused session, got %v", sess["status"])
	}

	// Resume with answers: extend the same fake's script and continue on the
	// same server (same DB, same session).
	fake.script = append(fake.script,
		agentRound{toolCalls: []openai.ToolCall{
			{ID: "c1", Type: "function", Function: openai.FunctionCall{
				Name: "create_node", Arguments: `{"id":"n2","type":"adapter","parent":"n1"}`,
			}},
		}},
		agentRound{content: strPtrA("完成")},
	)
	_, resume := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/resume", flowID),
		fmt.Sprintf(`{"provider_id":%d,"answers":[{"question_id":"auth-conflict-1","answer":"使用 token 认证"}]}`, providerID), http.StatusOK)
	if resume["finished"] != true {
		t.Fatalf("expected finished after resume, got %v", resume)
	}
}

func TestAgentPermissionDenied(t *testing.T) {
	fake := &agentFakeProvider{model: "m"}
	c := &client{t: t, ts: agentSharedServer(t, fake)}
	_, flowID, providerID := agentFixture(t, c)

	// An unprivileged user cannot submit agent edits.
	c2 := &client{t: t, ts: c.ts}
	c2.registerAndLoginAs("bob")
	c2.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID),
		fmt.Sprintf(`{"provider_id":%d,"instruction":"x"}`, providerID), http.StatusForbidden)
}

// blockAfterFirstFake emits one tool round then blocks on the second LLM call
// until ctx is done or released, letting tests hold the generation loop open.
type blockAfterFirstFake struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (f *blockAfterFirstFake) GetBaseURL() string { return "http://fake" }
func (f *blockAfterFirstFake) GetAPIKey() string  { return "sk" }
func (f *blockAfterFirstFake) GetModel() string   { return "m" }

func (f *blockAfterFirstFake) ChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest) (*openai.CompletionResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if n == 1 {
		msg := openai.Message{ToolCalls: []openai.ToolCall{
			{ID: "c1", Type: "function", Function: openai.FunctionCall{
				Name: "create_node", Arguments: `{"id":"n2","type":"adapter","parent":"n1"}`,
			}},
		}}
		return &openai.CompletionResponse{Choices: []struct {
			Message openai.Message `json:"message"`
		}{{Message: msg}}}, nil
	}
	select {
	case <-f.release:
	case <-ctx.Done():
	}
	return &openai.CompletionResponse{Choices: []struct {
		Message openai.Message `json:"message"`
	}{{Message: openai.Message{Content: strPtrA("完成")}}}}, nil
}

func (f *blockAfterFirstFake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestAgentSubmitSSEDisconnectReleasesLock verifies that when a client
// disconnects mid-stream, the generation loop terminates (no further LLM
// calls), the session lock is only released after the loop ends, and a new
// submission can then acquire it.
func TestAgentSubmitSSEDisconnectReleasesLock(t *testing.T) {
	fake := &blockAfterFirstFake{release: make(chan struct{})}
	c := &client{t: t, ts: agentSharedServer(t, fake)}
	_, flowID, providerID := agentFixture(t, c)

	ctx, cancel := context.WithCancel(context.Background())

	body := fmt.Sprintf(`{"provider_id":%d,"instruction":"加一个 adapter","mode":"edit"}`, providerID)
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ts.URL+fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID),
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do sse request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read the first streamed event so the loop is confirmed running.
	rd := bufio.NewReader(resp.Body)
	line, err := rd.ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read first event: %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("expected an SSE data line, got %q", line)
	}

	// Give the loop a moment to reach the second (blocked) LLM call.
	deadline := time.Now().Add(2 * time.Second)
	for fake.callCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fake.callCount() < 2 {
		t.Fatalf("generation loop did not reach the blocked call, calls=%d", fake.callCount())
	}

	// Disconnect: cancel the client context and close the body so the server
	// observes the connection going away.
	cancel()
	resp.Body.Close()

	// The loop must not make further LLM calls after the disconnect.
	time.Sleep(200 * time.Millisecond)
	after := fake.callCount()
	time.Sleep(200 * time.Millisecond)
	if fake.callCount() != after {
		t.Fatalf("loop kept calling LLM after disconnect: %d -> %d", after, fake.callCount())
	}

	// The lock must eventually be released so a new submission can run.
	released := false
	for i := 0; i < 100; i++ {
		unlock := service.LockFlow(flowID)
		if unlock != nil {
			unlock()
			released = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !released {
		t.Fatal("session lock not released after disconnect")
	}
}

func strPtrA(s string) *string { return &s }

var _ = model.RoleRead
