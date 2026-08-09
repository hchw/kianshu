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
	var toolEvents []Event
	for _, ev := range events {
		if ev.Kind == EventKindTool || ev.Kind == "" {
			toolEvents = append(toolEvents, ev)
		}
	}
	if len(toolEvents) != 1 {
		t.Fatalf("expected 1 tool event, got %d (%+v)", len(toolEvents), events)
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

	// 验证 session 状态为 paused 且包含 limit 类型的问题
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Status != model.SessionPaused {
		t.Fatalf("expected paused session after limit, got %s", sess.Status)
	}
	var questions []PauseQuestion
	_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	if len(questions) != 1 || questions[0].Type != "limit" {
		t.Fatalf("expected 1 limit question, got %+v", questions)
	}
	if questions[0].ID != "limit-1" {
		t.Fatalf("expected limit-1 id, got %q", questions[0].ID)
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
			"n1":  flow.NewNode("n1", flow.NodeStart),
			"n2":  flow.NewNode("n2", flow.NodeAPI),
		},
	}
	csCfg, err := flow.MarshalConfig(map[string]any{"writes": map[string]string{"token": "$.token"}})
	if err != nil {
		t.Fatal(err)
	}
	tree.Nodes["cs1"] = flow.NewNode("cs1", flow.NodeCacheSet)
	tree.Nodes["cs1"].Config = csCfg
	tree.Nodes["cs1"].Parent = "n1"
	tree.Nodes["n1"].Children = []string{"cs1"}
	tree.Nodes["n2"].Parent = "cs1"
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

// streamingFakeProvider implements StreamingProvider: each round's scripted
// content is delivered to onChunk character by character, mimicking an SSE
// delta stream.
type streamingFakeProvider struct {
	fakeProvider
}

func (f *streamingFakeProvider) StreamChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest, onChunk openai.StreamCallback) (*openai.CompletionResponse, error) {
	f.visited = append(f.visited, req)
	var round scriptedRound
	if len(f.script) > 0 {
		round = f.script[0]
		f.script = f.script[1:]
	}
	if round.err != nil {
		return nil, round.err
	}
	if round.content != nil {
		for _, r := range *round.content {
			onChunk(string(r))
		}
	}
	return &openai.CompletionResponse{
		Choices: []struct {
			Message openai.Message `json:"message"`
		}{{Message: openai.Message{Content: round.content, ToolCalls: round.toolCalls}}},
	}, nil
}

// TestRunAgentStreamsText verifies the streaming path: the assistant's reply
// is pushed token by token as round/text events, deep thinking is disabled on
// the wire, and the assembled result still lands in the draft/session.
func TestRunAgentStreamsText(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &streamingFakeProvider{fakeProvider: fakeProvider{
		model: "m",
		script: []scriptedRound{
			{toolCalls: []openai.ToolCall{
				tc(toolCreateNode, `{"id":"n2","type":"adapter","parent":"n1"}`),
			}},
			{content: strPtrS("已生成完成,请校验")},
		},
	}}
	var events []Event
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "加一个 adapter",
		Mode:        ModeEdit,
		Emit:        func(ev Event) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}

	// Streaming went through the provider: round -> text -> tool ordering.
	if len(events) < 3 {
		t.Fatalf("expected round/text/tool events, got %+v", events)
	}
	if events[0].Kind != EventKindRound || events[0].Round != 1 {
		t.Fatalf("expected round event first, got %+v", events[0])
	}
	var text strings.Builder
	var toolCount int
	for _, ev := range events {
		switch ev.Kind {
		case EventKindText:
			text.WriteString(ev.Text)
		case EventKindTool:
			toolCount++
		}
	}
	if text.String() != "已生成完成,请校验" {
		t.Fatalf("streamed text mismatch: %q", text.String())
	}
	if toolCount != 1 {
		t.Fatalf("expected 1 tool event, got %d", toolCount)
	}

	// Deep thinking disabled on the wire for every round.
	if len(fake.visited) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(fake.visited))
	}
	for i, req := range fake.visited {
		if req.Thinking == nil || req.Thinking.Type != "disabled" {
			t.Fatalf("round %d: expected thinking disabled, got %+v", i+1, req.Thinking)
		}
	}

	// Draft updated and session persisted as usual.
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)
	if tree.Nodes["n2"] == nil {
		t.Fatalf("draft not updated with new node")
	}
}

// TestGenerateFlowStreamsAnalysis verifies the generate workflow's analysis
// phase also streams round/text events before pausing.
func TestGenerateFlowStreamsAnalysis(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &streamingFakeProvider{fakeProvider: fakeProvider{
		model:   "m",
		script:  []scriptedRound{{content: strPtrS("Q: 认证方式冲突,请确认")}},
	}}
	var events []Event
	res, err := GenerateFlow(context.Background(), gdb, flowID, 1, "生成流程", fake, AgentHooks{Emit: func(ev Event) {
		events = append(events, ev)
	}})
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}
	if res.Finished {
		t.Fatalf("expected paused, got %+v", res)
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.Kind == EventKindText {
			text.WriteString(ev.Text)
		}
	}
	if text.String() != "Q: 认证方式冲突,请确认" {
		t.Fatalf("analysis text not streamed: %q", text.String())
	}
	// The pause event carries the analysis round number.
	if len(res.Events) != 1 || res.Events[0].Round != 1 {
		t.Fatalf("expected pause event at round 1, got %+v", res.Events)
	}
}

func TestSystemPromptContainsRoleAndPrinciples(t *testing.T) {
	p := systemPrompt(ModeEdit)
	if !strings.Contains(p, "测试流程编排专家") {
		t.Fatalf("system prompt should declare 测试流程编排专家 role")
	}
	if !strings.Contains(p, "不耻下问") {
		t.Fatalf("system prompt should contain 不耻下问 principle")
	}
	if !strings.Contains(p, "边界覆盖") {
		t.Fatalf("system prompt should contain 边界覆盖 principle")
	}
	if !strings.Contains(p, "数据流显式化") {
		t.Fatalf("system prompt should contain 数据流显式化 principle")
	}
	if !strings.Contains(p, "生成后自检") {
		t.Fatalf("system prompt should contain 生成后自检 principle")
	}
	if !strings.Contains(p, "生死之战") {
		t.Fatalf("system prompt should contain competitive urgency")
	}
	if !strings.Contains(p, "$cache.token") {
		t.Fatalf("system prompt should mention $cache.token for auth inputs")
	}
}

func TestSystemPromptCacheSetShape(t *testing.T) {
	p := systemPrompt(ModeEdit)
	// cache-set 的正确配置形状必须在提示中显式呈现(writes 键值映射,body.xxx 信封路径)
	if !strings.Contains(p, `"writes"`) {
		t.Fatalf("system prompt should teach cache-set config.writes shape")
	}
	if !strings.Contains(p, `"body.token"`) {
		t.Fatalf("system prompt should include writes example with body.token expression")
	}
	// 新特性：static 固定值 + $static.xxx 引用
	if !strings.Contains(p, `$static`) {
		t.Fatalf("system prompt should mention $static for literal string values")
	}
	if !strings.Contains(p, `"static"`) {
		t.Fatalf("system prompt should mention static config field")
	}
}

func TestSystemPromptAuthChainTemplate(t *testing.T) {
	p := systemPrompt(ModeGenerate)
	// 认证链模板:登录节点 + 缓存节点(writes 形状,body.xxx 信封路径) + 受保护节点($cache.token)
	for _, frag := range []string{"认证链", `"type":"cache-set"`, `"writes":{"token":"body.token"}`, `"$cache.token"`, `"parent":"n_cache_token"`} {
		if !strings.Contains(p, frag) {
			t.Fatalf("generate prompt should contain auth-chain template fragment %q", frag)
		}
	}
}

func TestSystemPromptGenerateMode(t *testing.T) {
	p := systemPrompt(ModeGenerate)
	if !strings.Contains(p, "启动 → 认证") {
		t.Fatalf("generate mode should include template order")
	}
}

func TestToolCreateNodeDescription(t *testing.T) {
	schemas := toolSchemas()
	var desc string
	for _, ts := range schemas {
		if ts.Function.Name == toolCreateNode {
			desc = ts.Function.Description
		}
	}
	if desc == "" {
		t.Fatal("create_node tool not found")
	}
	if !strings.Contains(desc, "inputs") {
		t.Fatalf("create_node description should mention inputs: %q", desc)
	}
	if !strings.Contains(desc, "config.params") {
		t.Fatalf("create_node description should mention config.params: %q", desc)
	}
	// cache-set 配置形状说明(cache-source-missing 修复的信息闭环)
	if !strings.Contains(desc, `"writes"`) {
		t.Fatalf("create_node description should teach cache-set writes shape: %q", desc)
	}
}

func TestToolUpdateNodeDescriptionMentionsCacheShape(t *testing.T) {
	schemas := toolSchemas()
	var desc string
	for _, ts := range schemas {
		if ts.Function.Name == toolUpdateNode {
			desc = ts.Function.Description
		}
	}
	if desc == "" {
		t.Fatal("update_node tool not found")
	}
	if !strings.Contains(desc, `"writes"`) {
		t.Fatalf("update_node description should teach cache-set writes shape: %q", desc)
	}
}

func TestUnitBriefAuthLabel(t *testing.T) {
	units := []model.TestUnit{
		{ID: 1, Method: "GET", Path: "/protected", Slug: "get-protected", Tag: "biz", Name: "受保护接口", Security: `[{"BearerAuth":[]}]`},
		{ID: 2, Method: "POST", Path: "/login", Slug: "post-login", Tag: "auth", Name: "登录", Security: "null"},
	}
	briefs := toBriefs(units)
	if briefs[0].Auth != "token" {
		t.Fatalf("protected unit brief should carry auth=token, got %q", briefs[0].Auth)
	}
	if briefs[1].Auth != "" {
		t.Fatalf("public unit brief should not carry auth label, got %q", briefs[1].Auth)
	}
	// JSON 序列化时 auth 键仅出现在受保护单元(auth,omitempty)
	out, err := json.Marshal(briefs)
	if err != nil {
		t.Fatalf("marshal briefs: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal briefs: %v", err)
	}
	if _, has := decoded[0]["auth"]; !has {
		t.Fatalf("brief JSON should contain auth key for protected unit: %s", out)
	}
	if _, has := decoded[1]["auth"]; has {
		t.Fatalf("brief JSON should omit auth key for public unit: %s", out)
	}
}

// TestGenerateAnalysisTreePersists 回归:C1 修复——analysis 阶段(工具调用感知循环)
// LLM 提前建树时,树必须落盘,RunAgent 才能从完整树继续,而非 DB 里的空树。
// 修复前:树 A 的 create_node 修改丢失,落盘只剩 start(空树 validate 仍 valid=true)。
func TestGenerateAnalysisTreePersists(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	createArgs := `{"id":"n2","type":"api","parent":"n1","config":{"unit_id":1}}`
	prov := &fakeProvider{
		model: "fake",
		script: []scriptedRound{
			// analysis round 1:LLM 直接建树(C3 引导失效的场景)
			{content: strPtrS("分析中"), toolCalls: []openai.ToolCall{tc(toolCreateNode, createArgs)}},
			// analysis round 2:无工具调用,分析结束
			{content: strPtrS("分析完成,无冲突")},
			// RunAgent round 3:无工具调用,完成
			{content: strPtrS("完成")},
		},
	}
	res, err := GenerateFlow(context.Background(), gdb, flowID, 1, "测试", prov)
	if err != nil {
		t.Fatalf("GenerateFlow: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}
	d, err := GetDraft(gdb, flowID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		t.Fatalf("parse draft: %v", err)
	}
	if _, ok := tree.Nodes["n2"]; !ok {
		t.Fatalf("analysis-phase node lost after generate: %s", d.Tree)
	}
}

// TestAgentToolsScopePause 验证 scope 越界时返回暂停结果而非硬错误
func TestAgentToolsScopePause(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)
	tree.Nodes["n1"].Outputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive}}
	tree.Nodes["n2"] = flow.NewNode("n2", flow.NodeAdapter)
	tree.AddChild("n1", "n2")

	// scope 只包含 n1，不包含 n2
	scope := map[string]bool{"n1": true}
	ctx := &ToolContext{DB: gdb, TestSetID: 1, Tree: tree, Scope: scope}

	// update_node 越界 → 应返回 pause
	res := ExecTool(ctx, toolUpdateNode, json.RawMessage(`{"id":"n2","config":{"expr":"$"}}`))
	if res.OK {
		t.Fatalf("update outside scope should not be OK")
	}
	if !res.Paused {
		t.Fatalf("update outside scope should pause, got error: %s", res.Error)
	}
	if len(res.Questions) != 1 || res.Questions[0].Type != "scope" {
		t.Fatalf("expected 1 scope question, got %+v", res.Questions)
	}
	q := res.Questions[0]
	if q.NodeID != "n2" {
		t.Fatalf("expected node_id n2, got %s", q.NodeID)
	}
	if q.Operation != toolUpdateNode {
		t.Fatalf("expected operation update_node, got %s", q.Operation)
	}
	if len(q.Options) != 3 {
		t.Fatalf("expected 3 options (允许/拒绝/允许全部), got %v", q.Options)
	}
	if q.ToolArgs == "" {
		t.Fatal("expected tool_args to be preserved for replay")
	}

	// delete_node 越界 → 应返回 pause
	res = ExecTool(ctx, toolDeleteNode, json.RawMessage(`{"id":"n2"}`))
	if !res.Paused || len(res.Questions) != 1 {
		t.Fatalf("delete outside scope should pause, got %+v", res)
	}
	if res.Questions[0].Operation != toolDeleteNode {
		t.Fatalf("expected delete_node operation, got %s", res.Questions[0].Operation)
	}

	// link_nodes 越界 → 应返回 pause（n2 是 child，不在 scope）
	res = ExecTool(ctx, toolLinkNodes, json.RawMessage(`{"parent":"n1","child":"n2"}`))
	if !res.Paused || len(res.Questions) != 1 {
		t.Fatalf("link outside scope should pause, got %+v", res)
	}
}

// TestRunAgentScopePauseAndResume 验证 RunAgent 中 scope 越界暂停→恢复的完整流程
func TestRunAgentScopePauseAndResume(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	// 预置树：n1(start) + n2(adapter)，scope 只勾选 n1
	d, _ := GetDraft(gdb, flowID)
	tree, _ := flow.ParseTree(d.Tree)
	tree.Nodes["n1"].Outputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive}}
	tree.Nodes["n2"] = flow.NewNode("n2", flow.NodeAdapter)
	tree.AddChild("n1", "n2")
	d.Tree = tree.String()
	if err := gdb.Save(d).Error; err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// Round 1: Agent 尝试 update_node n2 → 越界暂停
	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{toolCalls: []openai.ToolCall{
				tc(toolUpdateNode, `{"id":"n2","config":{"expr":"$"}}`),
			}},
		},
	}
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:      fake,
		Instruction:   "修改 n2",
		Mode:          ModeEdit,
		SelectedNodes: []string{"n1"},
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Finished {
		t.Fatal("expected paused, got finished")
	}
	if !strings.Contains(res.Message, "已暂停") {
		t.Fatalf("expected pause message, got %q", res.Message)
	}

	// 验证会话状态
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Status != model.SessionPaused {
		t.Fatalf("expected paused session, got %s", sess.Status)
	}
	var questions []PauseQuestion
	_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	if len(questions) != 1 || questions[0].Type != "scope" {
		t.Fatalf("expected 1 scope question, got %+v", questions)
	}
	if questions[0].ToolCallID == "" {
		t.Fatal("expected ToolCallID to be populated")
	}

	// 恢复：用户回答"允许"
	fake2 := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("已修改完成")},
		},
	}
	res2, err := ResumeGeneration(context.Background(), gdb, flowID, 1,
		[]PauseAnswer{{QuestionID: questions[0].ID, Answer: "允许"}}, fake2)
	if err != nil {
		t.Fatalf("ResumeGeneration: %v", err)
	}
	if !res2.Finished {
		t.Fatalf("expected finished after resume, got %+v", res2)
	}

	// 验证 n2 的 config 已被重放修改
	d2, _ := GetDraft(gdb, flowID)
	tree2, _ := flow.ParseTree(d2.Tree)
	if tree2.Nodes["n2"] == nil {
		t.Fatal("n2 should still exist after resume")
	}
}

// TestRunAgentModeGenerateIgnoresScope 验证生成模式下 scope 强制为空
func TestRunAgentModeGenerateIgnoresScope(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	fake := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("完成")},
		},
	}
	// 即使传入了 SelectedNodes，生成模式下也不应有任何 scope 限制
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:      fake,
		Instruction:   "生成流程",
		Mode:          ModeGenerate,
		SelectedNodes: []string{"n1"},
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.Finished {
		t.Fatalf("expected finished, got %+v", res)
	}
	// 不应有任何 tool event（因为 LLM 直接返回了 content 无 tool calls）
	// 重点：不应触发 scope 越界暂停
}

// TestRunAgentLimitPauseAndResume 验证 50 轮上限暂停→恢复的完整流程
func TestRunAgentLimitPauseAndResume(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	// 48 轮工具调用 + 第 49 轮 content 完成 = 刚好不触发上限
	script := make([]scriptedRound, MaxRounds)
	for i := range script {
		script[i] = scriptedRound{toolCalls: []openai.ToolCall{
			tc(toolCreateNode, fmt.Sprintf(`{"id":"n%d","type":"adapter","parent":"n1"}`, i+2)),
		}}
	}
	fake := &fakeProvider{model: "m", script: script}

	// 第一次运行：达到 50 轮上限 → 暂停
	res, err := RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "建很多节点",
		Mode:        ModeEdit,
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !res.LimitReached {
		t.Fatal("expected limit reached")
	}
	if res.Finished {
		t.Fatal("expected not finished")
	}

	// 验证暂停问题
	sess, _ := GetFlowSession(gdb, flowID, 1)
	var questions []PauseQuestion
	_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	if len(questions) != 1 || questions[0].Type != "limit" {
		t.Fatalf("expected limit question, got %+v", questions)
	}

	// 恢复：用户回答"继续生成"
	fake2 := &fakeProvider{
		model: "m",
		script: []scriptedRound{
			{content: strPtrS("生成完毕")},
		},
	}
	res2, err := ResumeGeneration(context.Background(), gdb, flowID, 1,
		[]PauseAnswer{{QuestionID: "limit-1", Answer: "继续生成"}}, fake2)
	if err != nil {
		t.Fatalf("ResumeGeneration: %v", err)
	}
	if !res2.Finished {
		t.Fatalf("expected finished after resume, got %+v", res2)
	}

	// 验证 session 恢复正常
	sess2, _ := GetFlowSession(gdb, flowID, 1)
	if sess2.Status != model.SessionActive {
		t.Fatalf("expected active session after resume, got %s", sess2.Status)
	}
}

// TestRunAgentLimitPauseStop 验证用户选择"停止"时优雅退出
func TestRunAgentLimitPauseStop(t *testing.T) {
	gdb := agentTestDB(t)
	flowID := createAgentFlow(t, gdb)

	script := make([]scriptedRound, MaxRounds)
	for i := range script {
		script[i] = scriptedRound{toolCalls: []openai.ToolCall{
			tc(toolCreateNode, fmt.Sprintf(`{"id":"n%d","type":"adapter","parent":"n1"}`, i+2)),
		}}
	}
	fake := &fakeProvider{model: "m", script: script}

	RunAgent(context.Background(), gdb, flowID, 1, AgentOptions{
		Provider:    fake,
		Instruction: "建很多节点",
		Mode:        ModeEdit,
	})

	// 用户选择"停止"
	fake2 := &fakeProvider{model: "m"}
	res, err := ResumeGeneration(context.Background(), gdb, flowID, 1,
		[]PauseAnswer{{QuestionID: "limit-1", Answer: "停止"}}, fake2)
	if err != nil {
		t.Fatalf("ResumeGeneration: %v", err)
	}
	if !res.Finished {
		t.Fatal("expected finished after stop")
	}
	if !strings.Contains(res.Message, "已停止") {
		t.Fatalf("expected stop message, got %q", res.Message)
	}

	// session 应恢复为 active
	sess, _ := GetFlowSession(gdb, flowID, 1)
	if sess.Status != model.SessionActive {
		t.Fatalf("expected active session after stop, got %s", sess.Status)
	}
}
