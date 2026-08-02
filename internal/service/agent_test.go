package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	"gorm.io/gorm"
)

// fakeProvider scripted ChatProvider for agent tests.
type fakeProvider struct {
	model   string
	script  []scriptedRound
	visited []openai.CompletionRequest
}

type scriptedRound struct {
	content   *string
	toolCalls []openai.ToolCall
	err       error
}

func (f *fakeProvider) GetBaseURL() string { return "http://fake.local" }
func (f *fakeProvider) GetAPIKey() string  { return "sk-test" }
func (f *fakeProvider) GetModel() string   { return f.model }

func (f *fakeProvider) ChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest) (*openai.CompletionResponse, error) {
	f.visited = append(f.visited, req)
	var round scriptedRound
	if len(f.script) > 0 {
		round = f.script[0]
		f.script = f.script[1:]
	}
	if round.err != nil {
		return nil, round.err
	}
	return &openai.CompletionResponse{
		Choices: []struct {
			Message openai.Message `json:"message"`
		}{{Message: openai.Message{Content: round.content, ToolCalls: round.toolCalls}}},
	}, nil
}

func tc(name, args string) openai.ToolCall {
	return openai.ToolCall{ID: "call_1", Type: "function", Function: openai.FunctionCall{Name: name, Arguments: args}}
}

func strPtrS(s string) *string { return &s }

// agentTestDB seeds a test set with one unit and a flow.
func agentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb := testDB(t)
	set := model.TestSet{Name: "demo", OwnerID: 1}
	if err := gdb.Create(&set).Error; err != nil {
		t.Fatalf("create set: %v", err)
	}
	unit := model.TestUnit{
		TestSetID: set.ID, Method: "POST", Path: "/login",
		Slug: "post-login", Tag: "auth", Name: "登录",
		Params: `{"login":{"name":"body","in":"body","required":true,"schema":{"type":"object"}}}`,
	}
	if err := gdb.Create(&unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}
	return gdb
}

func createAgentFlow(t *testing.T, gdb *gorm.DB) uint {
	t.Helper()
	var set model.TestSet
	if err := gdb.First(&set).Error; err != nil {
		t.Fatalf("no set: %v", err)
	}
	f, err := CreateFlow(gdb, set.ID, 1, "agent flow")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	return f.ID
}

func TestAgentToolsCreateAndLink(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)

	ctx := &ToolContext{DB: gdb, TestSetID: 1, Tree: tree}

	// list_units returns the seeded unit.
	res := ExecTool(ctx, toolListUnits, json.RawMessage(`{}`))
	if !res.OK {
		t.Fatalf("list_units failed: %v", res.Error)
	}
	data := mustJSON(res.Data)
	if !strings.Contains(data, `"/login"`) {
		t.Fatalf("list_units missing unit: %s", data)
	}

	// create an api node referencing the unit, link under start.
	res = ExecTool(ctx, toolCreateNode, json.RawMessage(`{"id":"n2","type":"api","parent":"n1","config":{"unit_id":1}}`))
	if !res.OK {
		t.Fatalf("create_node failed: %v", res.Error)
	}
	if tree.Nodes["n2"].Parent != "n1" {
		t.Fatalf("node n2 not linked under n1")
	}
	res = ExecTool(ctx, toolValidate, nil)
	if res.OK != true {
		t.Fatalf("validate failed: %v", res.Error)
	}
}

func TestAgentToolsScopeLimitsEdits(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)
	tree.Nodes["n1"].Outputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive}}
	// A pre-existing node outside the user's selected scope.
	tree.Nodes["n2"] = flow.NewNode("n2", flow.NodeAdapter)
	tree.AddChild("n1", "n2")

	scope := map[string]bool{"n1": true}
	ctx := &ToolContext{DB: gdb, TestSetID: 1, Tree: tree, Scope: scope}

	// Pre-existing node outside scope cannot be updated.
	res := ExecTool(ctx, toolUpdateNode, json.RawMessage(`{"id":"n2","config":{"expr":"$"}}`))
	if res.OK {
		t.Fatalf("update outside scope should fail")
	}

	// create inside scope (auto-joins) then update it.
	res = ExecTool(ctx, toolCreateNode, json.RawMessage(`{"id":"n3","type":"adapter"}`))
	if !res.OK {
		t.Fatalf("create in scope should be ok: %v", res.Error)
	}
	if !scope["n3"] {
		t.Fatalf("new node should join scope")
	}
	res = ExecTool(ctx, toolUpdateNode, json.RawMessage(`{"id":"n3","config":{"expr":"$"}}`))
	if !res.OK {
		t.Fatalf("update node in scope should be ok: %v", res.Error)
	}
}

func TestRunAgentEndToEnd(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &fakeProvider{
		model: "test-model",
		script: []scriptedRound{
			{
				toolCalls: []openai.ToolCall{
					tc(toolCreateNode, `{"id":"n2","type":"adapter","parent":"n1","outputs":{"greeting":{"type":"primitive"}}}`),
				},
			},
			{content: strPtrS("已生成完成")},
		},
	}
	var events []Event
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "给我加一个 adapter 节点",
		Mode:        ModeEdit,
		Emit:        func(ev Event) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)
	if tree.Nodes["n2"] == nil {
		t.Fatalf("draft not updated with new node")
	}
	// Session persisted.
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Messages == "" || sess.Messages == "[]" {
		t.Fatalf("session messages not persisted")
	}
}

func TestRunAgentLimitReached(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	// Every round calls create_node (50 rounds) and never finishes.
	script := make([]scriptedRound, MaxRounds)
	for i := range script {
		script[i] = scriptedRound{toolCalls: []openai.ToolCall{
			tc(toolCreateNode, fmt.Sprintf(`{"id":"n%d","type":"adapter","parent":"n1"}`, i+2)),
		}}
	}
	fake := &fakeProvider{model: "m", script: script}
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "建 50 个节点",
		Mode:        ModeEdit,
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.LimitReached {
		t.Fatalf("expected limit reached, got %+v", res)
	}
	if res.Rounds != MaxRounds {
		t.Fatalf("expected %d rounds, got %d", MaxRounds, res.Rounds)
	}
}

func TestRunAgentToolErrorRetry(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			// First round: unknown tool -> error fed back.
			{toolCalls: []openai.ToolCall{tc("bogus_tool", `{}`)}},
			// Second round: valid call, LLM learns format.
			{toolCalls: []openai.ToolCall{
				tc(toolCreateNode, `{"id":"n2","type":"adapter","parent":"n1"}`),
			}},
			{content: strPtrS("done")},
		},
	}
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "建一个 adapter",
		Mode:        ModeEdit,
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished after retry, got %+v", res)
	}
	// The bogus tool result should be an error event.
	if !res.Events[0].Result.(*ToolResult).OK {
		// error correctly fed back
	}
}

func TestGenerateFlowPausesOnConflict(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("Q: 该接口声明 apikey 认证,但实际为 token 认证,请确认认证方式\nQ: 请求参数 login 缺少示例值,请提供")},
		},
	}
	res, err := GenerateFlow(context.Background(), gdb, flowID, 1, "生成登录流程", fake)
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}
	if res.Finished {
		t.Fatalf("expected paused, got finished")
	}
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Status != model.SessionPaused {
		t.Fatalf("expected paused session, got %s", sess.Status)
	}
	var questions []PauseQuestion
	_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	if len(questions) != 2 {
		t.Fatalf("expected 2 pause questions, got %d", len(questions))
	}
	if questions[0].Type != "auth" {
		t.Fatalf("expected auth type, got %s", questions[0].Type)
	}
	// 7.1: ids are stable business identifiers, not bare counters.
	if questions[0].ID != "auth-conflict-1" {
		t.Fatalf("expected auth-conflict-1 id, got %q", questions[0].ID)
	}
	if questions[1].ID != "missing-param-1" {
		t.Fatalf("expected missing-param-1 id, got %q", questions[1].ID)
	}
}

func TestResumeGenerationSkipsUnanswered(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	// Two pause questions from the analysis.
	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("Q: 认证方式冲突,请确认\nQ: 请求参数 login 缺少示例值,请提供")},
		},
	}
	_, err := GenerateFlow(context.Background(), gdb, flowID, 1, "生成流程", fake)
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}

	// Resume answering only the auth question; the param question is skipped.
	fake2 := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{toolCalls: []openai.ToolCall{
				tc(toolCreateNode, `{"id":"n2","type":"adapter","parent":"n1"}`),
			}},
			{content: strPtrS("完成")},
		},
	}
	res, err := ResumeGeneration(context.Background(), gdb, flowID, 1,
		[]PauseAnswer{{QuestionID: "auth-conflict-1", Answer: "使用 token 认证"}}, fake2)
	if err != nil {
		t.Fatalf("ResumeGeneration: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}
	if !strings.Contains(res.Message, "missing-param-1") {
		t.Fatalf("expected skipped question to be reported, got message %q", res.Message)
	}
	// The unanswered question must not be silently fed to the model: assert the
	// last user message lists it as skipped.
	last := fake2.visited[len(fake2.visited)-1]
	foundSkipped := false
	for _, m := range last.Messages {
		if m.Content != nil && strings.Contains(*m.Content, "未回答,跳过") && strings.Contains(*m.Content, "missing-param-1") {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatal("expected the skipped question to be marked in the resume message")
	}
}

func TestGenerateFlowDetectsAuthConflictWithoutQLine(t *testing.T) {
	gdb := agentTestDB(t)

	// A second unit that declares apikey security.
	var set model.TestSet
	if err := gdb.First(&set).Error; err != nil {
		t.Fatalf("no set: %v", err)
	}
	apikeyUnit := model.TestUnit{
		TestSetID: set.ID, Method: "GET", Path: "/orders", Slug: "get-orders",
		Tag: "biz", Name: "订单列表", Security: `[{"apikey":[]}]`,
	}
	if err := gdb.Create(&apikeyUnit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}

	// Flow draft already establishes token auth: a cache-set writes "token"
	// and an api node consumes $cache.token for its authorization input.
	flowID := createAgentFlow(t, gdb)
	tree := &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
			"n2": flow.NewNode("n2", flow.NodeAPI),
		},
		CacheSets: []string{"cs1"},
	}
	csCfg, err := flow.MarshalConfig(map[string]any{"writes": map[string]string{"token": "$.token"}})
	if err != nil {
		t.Fatal(err)
	}
	tree.Nodes["cs1"] = flow.NewNode("cs1", flow.NodeCacheSet)
	tree.Nodes["cs1"].Config = csCfg
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"authorization": {Type: flow.IOTypePrimitive, Source: "$cache.token"},
	}
	tree.AddChild("n1", "n2")
	d, err := GetDraft(gdb, flowID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	d.Tree = tree.String()
	if err := gdb.Save(d).Error; err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// The LLM says nothing about a conflict — the code check must still pause.
	fake := &fakeProvider{
		model:   "m",
		script:  []scriptedRound{{content: strPtrS("开始生成")}},
	}
	res, err := GenerateFlow(context.Background(), gdb, flowID, 1, "生成订单流程", fake)
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}
	if res.Finished {
		t.Fatalf("expected code-detected pause, got finished")
	}
	sess, _ := GetFlowSession(gdb, flowID, 1)
	var questions []PauseQuestion
	_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	if len(questions) != 1 {
		t.Fatalf("expected 1 code-detected question, got %d", len(questions))
	}
	if questions[0].ID != "auth-conflict-1" || questions[0].Type != "auth" {
		t.Fatalf("unexpected question: %+v", questions[0])
	}
	if !strings.Contains(questions[0].Question, "apikey") || !strings.Contains(questions[0].Question, "token") {
		t.Fatalf("expected mismatch description, got %q", questions[0].Question)
	}
}

func TestResumeGenerationContinues(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	// First: generation pauses.
	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("Q: 认证方式冲突,请确认")},
		},
	}
	_, err := GenerateFlow(context.Background(), gdb, flowID, 1, "生成流程", fake)
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}

	// Resume with answers, then agent creates a node and finishes.
	fake2 := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{toolCalls: []openai.ToolCall{
				tc(toolCreateNode, `{"id":"n2","type":"adapter","parent":"n1"}`),
			}},
			{content: strPtrS("完成")},
		},
	}
	res, err := ResumeGeneration(context.Background(), gdb, flowID, 1, []PauseAnswer{{QuestionID: "auth-conflict-1", Answer: "使用 token 认证"}}, fake2)
	if err != nil {
		t.Fatalf("ResumeGeneration: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Status != model.SessionActive {
		t.Fatalf("expected active session after resume, got %s", sess.Status)
	}
}
