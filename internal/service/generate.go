package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	"gorm.io/gorm"
)

// PauseQuestion is one user-decision point surfaced during generation.
type PauseQuestion struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // auth | sample | order | swagger
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
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
func GenerateFlow(ctx context.Context, db *gorm.DB, flowID, userID uint, instruction string, provider ChatProvider) (*AgentResult, error) {
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
			"若存在,先列出待确认问题(每行以 'Q: ' 开头);否则直接开始按固定顺序生成:启动→认证取token/写缓存→业务序列→断言→收尾。",
		len(units), formatUnitBriefs(units), d.Tree, instruction)

	// ② + ③ analyze + conflict detection via a single structured round.
	session, err := GetFlowSession(db, flowID, userID)
	if err != nil {
		return nil, err
	}
	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}
	req := openai.CompletionRequest{
		Model:    provider.GetModel(),
		Messages: append([]openai.Message{{Role: "system", Content: strPtr(systemPrompt(ModeGenerate))}}, history...),
		Tools:    toolSchemas(),
	}
	req.Messages = append(req.Messages, openai.Message{Role: "user", Content: strPtr(contextMsg)})

	resp, err := provider.ChatCompletion(ctx, provider, req)
	if err != nil {
		return nil, err
	}
	analysis := resp.Choices[0].Message
	req.Messages = append(req.Messages, analysis)

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
			Rounds:   1,
			Finished: false,
			Message:  "生成已暂停,需要用户确认以下问题",
			Events: []Event{{
				Round: 1, Tool: "analyze",
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
	return RunAgent(ctx, db, flowID, userID, AgentOptions{
		Provider:       provider,
		Instruction:    instruction,
		Mode:           ModeGenerate,
		PresetMessages: req.Messages[1:],
	})
}

// ResumeGeneration continues a paused generation with the user's answers. Each
// answer carries the id of the question it addresses; pending questions without
// an answer are listed as explicitly skipped so nothing is silently misapplied.
func ResumeGeneration(ctx context.Context, db *gorm.DB, flowID, userID uint, answers []PauseAnswer, provider ChatProvider) (*AgentResult, error) {
	session, err := GetFlowSession(db, flowID, userID)
	if err != nil {
		return nil, err
	}
	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}
	req := openai.CompletionRequest{
		Model:    provider.GetModel(),
		Messages: append([]openai.Message{{Role: "system", Content: strPtr(systemPrompt(ModeGenerate))}}, history...),
		Tools:    toolSchemas(),
	}

	answered := map[string]string{}
	for _, a := range answers {
		answered[a.QuestionID] = a.Answer
	}
	var pending []PauseQuestion
	_ = json.Unmarshal([]byte(session.PendingQuestions), &pending)

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
	})
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 {
		res.Message = fmt.Sprintf("已跳过未回答的问题:%s;%s", strings.Join(skipped, ", "), res.Message)
	}
	return res, nil
}

// formatUnitBriefs renders unit summaries compactly for the generation prompt.
func formatUnitBriefs(units []model.TestUnit) string {
	out := ""
	for _, u := range units {
		out += fmt.Sprintf("- #%d %s %s (tag=%s, name=%s)\n", u.ID, u.Method, u.Path, u.Tag, u.Name)
	}
	if out == "" {
		out = "(无)"
	}
	return out
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
		Writes map[string]string `json:"writes"`
	}
	tokenKeys := map[string]bool{}
	for _, csID := range tree.CacheSets {
		cs, ok := tree.Nodes[csID]
		if !ok || cs == nil || cs.Type != flow.NodeCacheSet {
			continue
		}
		cfg.Writes = nil
		_ = flow.UnmarshalConfig(cs, &cfg)
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
	for _, token := range []string{"authorization", "token", "api-key", "apikey", "cookie", "auth"} {
		if strings.Contains(lk, token) {
			return true
		}
	}
	return false
}
