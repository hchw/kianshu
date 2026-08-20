package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// PauseQuestion is one user-decision point surfaced during generation.
type PauseQuestion struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"` // auth | sample | order | swagger | scope | limit
	Question   string   `json:"question"`
	Options    []string `json:"options,omitempty"`
	NodeID     string   `json:"node_id,omitempty"`
	NodeType   string   `json:"node_type,omitempty"`
	Operation  string   `json:"operation,omitempty"`
	ToolArgs   string   `json:"tool_args,omitempty"`    // JSON-encoded tool call args for replay
	ToolCallID string   `json:"tool_call_id,omitempty"` // LLM tool call id for tool-result correlation
}

// PauseAnswer pairs a user's answer with the question it addresses. Mapping by
// question id keeps each answer attributable to exactly one pause point.
type PauseAnswer struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}

// GenerateFlow runs the fixed generation workflow (D6): intent -> filter ->
// analyze -> conflict detection -> pause -> fixed-order generation -> validate
// -> land draft. Conflicts stop the run at a pause point awaiting answers.
// Optional hooks stream analysis rounds (LLM 回馈文本与轮次) in real time.
func GenerateFlow(ctx context.Context, db *gorm.DB, flowID, userID uint, instruction string, provider ChatProvider, hooks ...AgentHooks) (*AgentResult, error) {
	h := AgentHooks{}
	if len(hooks) > 0 {
		h = hooks[0]
	}
	// ① Collect intent: units + existing flow become the generation context.
	tsID := flowTestSetID(db, flowID)
	units, err := listUnitsQuery(db, tsID, "", "")
	if err != nil {
		return nil, err
	}
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	contextMsg := fmt.Sprintf(
		"测试集共 %d 个测试单元(unit_id=ID):\n%s当前流草稿:\n%s\n用户用例: %s\n\n"+
			"请先分析是否存在需要用户决策的冲突:认证方式冲突(apikey↔token)、参数缺失(需要示例值)、业务步骤顺序不定、swagger 语义不清。"+
			"若存在,先列出待确认问题(每行以 'Q: ' 开头);否则直接开始按固定顺序生成:启动→认证取token/写缓存→业务序列→断言→收尾。"+
			"生成完成后必须用 update_node 给 start 节点写入 config.params(测试初值),覆盖下游所有 api 节点的业务参数。",
		len(units), formatUnitBriefs(units), d.Tree, instruction)

	// ② + ③ analyze + conflict detection via a tool-call-aware loop.
	session, err := GetFlowSession(db, flowID, userID)
	if err != nil {
		return nil, err
	}
	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	toolCtx := &ToolContext{DB: db, TestSetID: tsID, Tree: tree}
	req := newAgentCompletionRequest(provider, append([]openai.Message{{Role: "system", Content: strPtr(flowSystemPrompt(ModeGenerate, d.SystemPrompt))}}, history...))
	req.Tools = toolSchemas()
	req.Messages = append(req.Messages, openai.Message{Role: "user", Content: strPtr(contextMsg)})

	const maxAnalysisRounds = 5
	var analysis openai.Message
	analysisRounds := 0
	for round := 1; round <= maxAnalysisRounds; round++ {
		analysisRounds = round
		h.emit(Event{Kind: EventKindRound, Round: round})

		// 3 次重试应对瞬态失败
		var resp *openai.CompletionResponse
		var lastErr error
		for retry := 0; retry < 3; retry++ {
			resp, lastErr = generateComplete(ctx, provider, req, func(text string) {
				h.emit(Event{Kind: EventKindText, Round: round, Text: text})
			})
			if lastErr == nil {
				break
			}
			if ctx.Err() != nil {
				break
			}
			if retry < 2 {
				log.Warnf("agent: generate flow=%d analysis round=%d LLM 调用失败(第%d次重试): %v", flowID, round, retry+1, lastErr)
				time.Sleep(time.Duration(retry+1) * 2 * time.Second)
			}
		}
		if lastErr != nil {
			log.Errorf("agent: generate flow=%d analysis round=%d LLM 调用失败(已重试3次): %v", flowID, round, lastErr)
			return nil, lastErr
		}
		analysis = resp.Choices[0].Message
		req.Messages = append(req.Messages, analysis)

		if len(analysis.ToolCalls) == 0 {
			break
		}
		log.Infof("agent: generate flow=%d analysis round=%d LLM 发起 %d 个工具调用", flowID, round, len(analysis.ToolCalls))
		for _, tc := range analysis.ToolCalls {
			log.Infof("agent: generate flow=%d analysis round=%d tool=%s args=%s", flowID, round, tc.Function.Name, tc.Function.Arguments)
			result := ExecTool(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			log.Infof("agent: generate flow=%d analysis round=%d tool=%s 结果 ok=%v err=%s", flowID, round, tc.Function.Name, result.OK, result.Error)
			h.emit(Event{Kind: EventKindTool, Round: round, Tool: tc.Function.Name, Args: tc.Function.Arguments, Result: result})
			req.Messages = append(req.Messages, openai.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    strPtr(jsonString(result)),
			})
		}
	}

	// ④ pause: merge LLM-raised Q: lines with code-level conflict detection.
	// The code check compares each unit's swagger security declaration against
	// the flow's actual auth approach, so a mismatch is flagged even when the
	// model fails to phrase it as a Q: line.
	questions := parsePauseQuestions(analysis.Content)
	questions = append(questions, detectAuthConflicts(units, d.Tree)...)
	if len(questions) > 0 {
		if b, err := json.Marshal(req.Messages[1:]); err == nil {
			session.Messages = string(b)
		}
		session.Status = model.SessionPaused
		if b, err := json.Marshal(questions); err == nil {
			session.PendingQuestions = string(b)
		}
		if err := SaveFlowSession(db, session); err != nil {
			return nil, err
		}
		return &AgentResult{
			Rounds:   analysisRounds,
			Finished: false,
			Message:  "生成已暂停,需要用户确认以下问题",
			Events: []Event{{
				Kind: EventKindTool, Round: analysisRounds, Tool: "analyze",
				Result: map[string]any{"paused": true, "questions": questions},
			}},
		}, nil
	}

	// ⑤⑥⑦ no conflicts: run the standard edit loop from the analysis message.
	if b, err := json.Marshal(req.Messages[1:]); err == nil {
		session.Messages = string(b)
	}
	if err := SaveFlowSession(db, session); err != nil {
		return nil, err
	}
	// Analysis 阶段(工具调用感知循环)可能已通过 create_node/update_node 修改了树
	// (LLM 可能提前建树)。若不落盘,RunAgent 会从 DB 重建空树,导致生成结果丢失——
	// 空树 validate 仍能通过(valid=true),因此必须在此持久化,保证编辑循环从完整树继续。
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, err
	}
	if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt); err != nil {
		return nil, err
	}
	return RunAgent(ctx, db, flowID, userID, AgentOptions{
		Provider:       provider,
		Instruction:    instruction,
		Mode:           ModeGenerate,
		PresetMessages: req.Messages[1:],
		Emit:           h.Emit,
		StartRound:     analysisRounds + 1,
	})
}

// generateComplete calls the provider for the analysis phase, preferring
// streaming when available so the model's feedback reaches the client in real
// time.
func generateComplete(ctx context.Context, p ChatProvider, req openai.CompletionRequest, onText func(string)) (*openai.CompletionResponse, error) {
	if sp, ok := p.(StreamingProvider); ok {
		return sp.StreamChatCompletion(ctx, p, req, onText)
	}
	return p.ChatCompletion(ctx, p, req)
}

// ResumeGeneration continues a paused generation with the user's answers. Each
// answer carries the id of the question it addresses; pending questions without
// an answer are listed as explicitly skipped so nothing is silently misapplied.
// Scope-type pauses are handled by updating the node scope and replaying the
// intercepted tool call before resuming the LLM loop.
func ResumeGeneration(ctx context.Context, db *gorm.DB, flowID, userID uint, answers []PauseAnswer, provider ChatProvider, hooks ...AgentHooks) (*AgentResult, error) {
	h := AgentHooks{}
	if len(hooks) > 0 {
		h = hooks[0]
	}
	session, err := GetFlowSession(db, flowID, userID)
	if err != nil {
		return nil, err
	}
	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}

	answered := map[string]string{}
	for _, a := range answers {
		answered[a.QuestionID] = a.Answer
	}
	var pending []PauseQuestion
	_ = json.Unmarshal([]byte(session.PendingQuestions), &pending)

	// 检测 scope 越界暂停：不同于 generate 模式的 auth/sample 暂停，
	// scope 暂停需要重放被拦截的工具调用而非向 LLM 注入回答消息。
	hasScope := false
	hasLimit := false
	for _, q := range pending {
		switch q.Type {
		case "scope":
			hasScope = true
		case "limit":
			hasLimit = true
		}
	}

	if hasScope {
		return resumeScopePause(ctx, db, flowID, session, history, pending, answered, provider, h.Emit)
	}
	if hasLimit {
		return resumeLimitPause(ctx, db, flowID, session, history, pending, answered, provider, h.Emit)
	}

	// 原有的 generate 模式暂停恢复逻辑
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	req := newAgentCompletionRequest(provider, append([]openai.Message{{Role: "system", Content: strPtr(flowSystemPrompt(ModeGenerate, d.SystemPrompt))}}, history...))
	req.Tools = toolSchemas()

	var lines []string
	var skipped []string
	for _, q := range pending {
		if ans, ok := answered[q.ID]; ok {
			lines = append(lines, fmt.Sprintf("- [%s] %s → %s", q.ID, q.Question, ans))
		} else {
			lines = append(lines, fmt.Sprintf("- [%s] %s → (未回答,跳过)", q.ID, q.Question))
			skipped = append(skipped, q.ID)
		}
	}
	answerMsg := "用户对暂停点的回答如下,请据此继续生成:\n" + strings.Join(lines, "\n")
	req.Messages = append(req.Messages, openai.Message{Role: "user", Content: strPtr(answerMsg)})
	if b, err := json.Marshal(req.Messages[1:]); err == nil {
		session.Messages = string(b)
	}
	session.Status = model.SessionActive
	session.PendingQuestions = ""
	if err := SaveFlowSession(db, session); err != nil {
		return nil, err
	}
	first := ""
	for _, q := range pending {
		if ans, ok := answered[q.ID]; ok {
			first = ans
			break
		}
	}
	res, err := RunAgent(ctx, db, flowID, userID, AgentOptions{
		Provider:       provider,
		Instruction:    first,
		Mode:           ModeGenerate,
		PresetMessages: req.Messages[1:],
		Emit:           h.Emit,
	})
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 {
		res.Message = fmt.Sprintf("已跳过未回答的问题:%s;%s", strings.Join(skipped, ", "), res.Message)
	}
	return res, nil
}

// resumeScopePause handles a scope-violation pause: process the user's allow/deny
// answers, update the scope, replay the intercepted tool call, and resume the
// LLM loop in edit mode.
func resumeScopePause(ctx context.Context, db *gorm.DB, flowID uint, session *model.FlowSession, history []openai.Message, pending []PauseQuestion, answered map[string]string, provider ChatProvider, emit func(Event)) (*AgentResult, error) {
	// 解析草稿树以构建 ToolContext
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	tsID := flowTestSetID(db, flowID)

	// 从 session 恢复消息中的 scope 信息（通过节点 ID 重建原始 scope）
	scope := map[string]bool{}
	allowAll := false

	for _, q := range pending {
		ans, ok := answered[q.ID]
		if !ok {
			continue
		}
		switch ans {
		case "允许本次全部越界节点":
			allowAll = true
		case "允许":
			// 将越界节点加入 scope
			for _, nid := range strings.Split(q.NodeID, ",") {
				nid = strings.TrimSpace(nid)
				if nid != "" {
					scope[nid] = true
				}
			}
		case "拒绝":
			// 不加入 scope，稍后返回跳过结果
		}
	}

	if allowAll {
		scope = nil // nil 表示不限制
	}

	// 重放被拦截的工具调用
	toolCtx := &ToolContext{DB: db, TestSetID: tsID, Tree: tree, Scope: scope}

	// 构建暂停 tool_call_id 集合，用于过滤历史中的占位 tool 消息
	pendingIDs := map[string]bool{}
	for _, q := range pending {
		if q.ToolCallID != "" {
			pendingIDs[q.ToolCallID] = true
		}
	}

	// 从历史中移除占位 tool 消息（RunAgent scope 暂停时写入的
	// {"paused":true} 占位），然后追加真实 tool 结果
	extended := make([]openai.Message, 0, len(history)+len(pending))
	for _, m := range history {
		if m.Role == "tool" && pendingIDs[m.ToolCallID] {
			continue
		}
		extended = append(extended, m)
	}

	for _, q := range pending {
		ans, ok := answered[q.ID]
		if !ok || ans == "拒绝" {
			// 未回答或拒绝：注入跳过结果
			if q.Operation != "" && q.ToolArgs != "" {
				skipResult := &ToolResult{OK: false, Error: fmt.Sprintf("用户拒绝修改节点 %s", q.NodeID)}
				extended = append(extended, openai.Message{
					Role:       "tool",
					ToolCallID: q.ToolCallID,
					Content:    strPtr(jsonString(skipResult)),
				})
			}
			continue
		}
		// 允许：重放工具调用
		if q.Operation != "" && q.ToolArgs != "" {
			replayResult := ExecTool(toolCtx, q.Operation, json.RawMessage(q.ToolArgs))
			extended = append(extended, openai.Message{
				Role:       "tool",
				ToolCallID: q.ToolCallID,
				Content:    strPtr(jsonString(replayResult)),
			})
		}
	}

	// 保存会话
	if b, err := json.Marshal(extended); err == nil {
		session.Messages = string(b)
	}
	session.Status = model.SessionActive
	session.PendingQuestions = ""
	if err := SaveFlowSession(db, session); err != nil {
		return nil, err
	}

	return RunAgent(ctx, db, flowID, 0, AgentOptions{
		Provider:       provider,
		Instruction:    "继续",
		Mode:           ModeEdit,
		PresetMessages: extended,
		ResumeScope:    scope,
		Emit:           emit,
	})
}

// resumeLimitPause handles a round-limit pause: on "继续生成" resume the LLM
// loop with its full history; on "停止" mark the session active and stop.
func resumeLimitPause(ctx context.Context, db *gorm.DB, flowID uint, session *model.FlowSession, history []openai.Message, pending []PauseQuestion, answered map[string]string, provider ChatProvider, emit func(Event)) (*AgentResult, error) {
	ans, ok := answered["limit-1"]
	if !ok || ans == "停止" {
		session.Status = model.SessionActive
		session.PendingQuestions = ""
		if err := SaveFlowSession(db, session); err != nil {
			return nil, err
		}
		return &AgentResult{Finished: true, Message: "已停止生成"}, nil
	}

	// 继续：以完整历史继续
	if b, err := json.Marshal(history); err == nil {
		session.Messages = string(b)
	}
	session.Status = model.SessionActive
	session.PendingQuestions = ""
	if err := SaveFlowSession(db, session); err != nil {
		return nil, err
	}

	return RunAgent(ctx, db, flowID, 0, AgentOptions{
		Provider:       provider,
		Instruction:    "继续生成",
		Mode:           ModeEdit,
		PresetMessages: history,
		Emit:           emit,
	})
}

// formatUnitBriefs renders unit summaries compactly for the generation prompt,
// including parameter summaries and auth info.
func formatUnitBriefs(units []model.TestUnit) string {
	out := ""
	for _, u := range units {
		auth := securityScheme(u.Security)
		authLabel := ""
		if auth != "" {
			authLabel = ", auth=" + auth
		}
		paramSummary := paramBrief(u)
		out += fmt.Sprintf("- #%d %s %s (tag=%s, name=%s%s)\n", u.ID, u.Method, u.Path, u.Tag, u.Name, authLabel)
		if paramSummary != "" {
			out += "  params: [" + paramSummary + "]\n"
		}
	}
	if out == "" {
		out = "(无)"
	}
	return out
}

// paramBrief extracts a compact parameter summary from a unit's swagger params.
func paramBrief(u model.TestUnit) string {
	if u.Params == "" || u.Params == "null" {
		return ""
	}
	type p struct {
		Name string `json:"name"`
		In   string `json:"in"`
	}
	var params []p
	if err := json.Unmarshal([]byte(u.Params), &params); err != nil {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, pp := range params {
		parts = append(parts, pp.Name+"("+pp.In+")")
	}
	return strings.Join(parts, ", ")
}

// parsePauseQuestions extracts 'Q: ' lines the model raised during analysis.
// IDs are stable business identifiers derived from the question type (e.g.
// auth-conflict-1, missing-param-<unit>), never a bare per-session counter.
func parsePauseQuestions(content *string) []PauseQuestion {
	if content == nil {
		return nil
	}
	counts := map[string]int{}
	var out []PauseQuestion
	for _, line := range strings.Split(*content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Q:") {
			continue
		}
		text := strings.TrimSpace(line[2:])
		q := PauseQuestion{Question: text, Type: "sample"}
		switch {
		case strings.Contains(text, "认证"), strings.Contains(text, "auth"), strings.Contains(text, "apikey"), strings.Contains(text, "token"):
			q.Type = "auth"
		case strings.Contains(text, "顺序"), strings.Contains(text, "order"), strings.Contains(text, "步骤"):
			q.Type = "order"
		case strings.Contains(text, "swagger"), strings.Contains(text, "语义"), strings.Contains(text, "不标准"):
			q.Type = "swagger"
		case strings.Contains(text, "参数"), strings.Contains(text, "param"), strings.Contains(text, "示例"):
			q.Type = "sample"
		}
		counts[q.Type]++
		switch q.Type {
		case "auth":
			q.ID = fmt.Sprintf("auth-conflict-%d", counts[q.Type])
		case "order":
			q.ID = fmt.Sprintf("order-conflict-%d", counts[q.Type])
		case "swagger":
			q.ID = fmt.Sprintf("swagger-conflict-%d", counts[q.Type])
		default:
			q.ID = fmt.Sprintf("missing-param-%d", counts[q.Type])
		}
		out = append(out, q)
	}
	return out
}

// detectAuthConflicts runs code-level conflict detection: it compares each
// unit's swagger security declaration against the actual auth approach of the
// flow draft tree. A mismatch (e.g. the API declares apikey but the flow
// already obtains a bearer token) yields a pause question even when the LLM
// did not surface it as a 'Q:' line.
func detectAuthConflicts(units []model.TestUnit, draftTree string) []PauseQuestion {
	tree, err := flow.ParseTree(draftTree)
	if err != nil {
		return nil
	}
	flowAuth := treeAuthScheme(tree)
	if flowAuth == "" {
		return nil
	}
	count := 0
	var out []PauseQuestion
	for _, u := range units {
		declared := securityScheme(u.Security)
		if declared == "" || declared == flowAuth {
			continue
		}
		count++
		out = append(out, PauseQuestion{
			ID:       fmt.Sprintf("auth-conflict-%d", count),
			Type:     "auth",
			Question: fmt.Sprintf("单元 #%d %s %s 声明 %s 认证,但当前流程实际采用 %s,请确认认证方式", u.ID, u.Method, u.Path, declared, flowAuth),
		})
	}
	return out
}

// treeAuthScheme inspects the flow tree's auth wiring: when a cache-set writes
// a key consumed by an auth input (authorization/token), the flow authenticates
// with a bearer token; when auth inputs are fed only by start params, it uses a
// static apikey. Returns "" when the tree does not establish an auth approach.
func treeAuthScheme(tree *flow.Tree) string {
	var cfg struct {
		Writes map[string]any `json:"writes"`
	}
	tokenKeys := map[string]bool{}
	for _, n := range tree.Nodes {
		if n == nil || n.Type != flow.NodeCacheSet {
			continue
		}
		cfg.Writes = nil
		_ = flow.UnmarshalConfig(n, &cfg)
		for k := range cfg.Writes {
			if isAuthKey(k) {
				tokenKeys[k] = true
			}
		}
	}
	for _, n := range tree.Nodes {
		if n == nil || n.Type != flow.NodeAPI {
			continue
		}
		for key, io := range n.Inputs {
			if !isAuthKey(key) {
				continue
			}
			if ck, ok := flow.CacheKey(io.Source); ok && tokenKeys[ck] {
				return "token"
			}
		}
	}
	if len(tokenKeys) > 0 {
		return "apikey"
	}
	return ""
}

// securityScheme classifies a unit's swagger security declaration into
// "token" (bearer/oauth) or "apikey" (api key header), or "" when none.
func securityScheme(securityJSON string) string {
	var secs []map[string]any
	if err := json.Unmarshal([]byte(securityJSON), &secs); err != nil {
		return ""
	}
	for _, s := range secs {
		for name := range s {
			low := strings.ToLower(name)
			switch {
			case strings.Contains(low, "bearer"), strings.Contains(low, "oauth"), strings.Contains(low, "token"):
				return "token"
			case strings.Contains(low, "apikey"), strings.Contains(low, "api_key"), strings.Contains(low, "x-api-key"):
				return "apikey"
			}
		}
	}
	return ""
}

// isAuthKey recognizes header authentication keys such as authorization or
// token (mirrors the validator's and executor's rule).
func isAuthKey(key string) bool {
	lk := strings.ToLower(key)
	for _, token := range []string{"authorization", "token", "api-key", "apikey", "api_key", "cookie", "auth"} {
		if strings.Contains(lk, token) {
			return true
		}
	}
	return false
}
