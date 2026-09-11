package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/agent"
	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Case Flow Agent 工具名。
const (
	caseToolGetDocument = "get_background_document"
	caseToolSwagger     = "get_swagger_summary"
	caseToolGetTree     = "get_case_tree"
	caseToolAddNode     = "add_case_node"
	caseToolUpdateNode  = "update_case_node"
	caseToolMoveNode    = "move_case_node"
	caseToolDeleteNode  = "delete_case_node"
	caseToolReplaceTree = "replace_case_subtree"
	caseToolSetStatus   = "set_case_status"
	caseToolAskUser     = "ask_user_clarification"
)

var caseFlowLocks = agent.NewKeyedLock()

// LockCaseFlow serializes one case flow agent submission.
func LockCaseFlow(caseFlowID uint) func() { return caseFlowLocks.TryLock(caseFlowID) }

type caseToolContext struct {
	db         *gorm.DB
	caseFlowID uint
	userID     uint
	tree       caseflow.Tree
}

// CaseAgentOptions carries one case flow agent submission.
type CaseAgentOptions struct {
	Provider    ChatProvider
	Instruction string
	Thinking    string
	Emit        func(Event)
	StartRound  int
	Messages    []openai.Message // 可选：替换历史
}

// CaseAgentResult is the outcome of a case flow agent submission.
type CaseAgentResult struct {
	Rounds       int     `json:"rounds"`
	LimitReached bool    `json:"limit_reached"`
	Finished     bool    `json:"finished"`
	Message      string  `json:"message,omitempty"`
	Events       []Event `json:"events"`
}

// GetCaseFlowSession returns the case flow dialog session.
func GetCaseFlowSession(db *gorm.DB, caseFlowID, userID uint) (*model.CaseFlowSession, error) {
	var s model.CaseFlowSession
	err := db.Where("case_flow_id = ?", caseFlowID).First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s = model.CaseFlowSession{CaseFlowID: caseFlowID, CreatedBy: userID, Status: model.SessionActive, Messages: "[]"}
	if err := db.Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ResetCaseFlowSession clears dialog history.
func ResetCaseFlowSession(db *gorm.DB, caseFlowID uint) (*model.CaseFlowSession, error) {
	s, err := GetCaseFlowSession(db, caseFlowID, 0)
	if err != nil {
		return nil, err
	}
	s.Messages = "[]"
	s.PendingQuestions = ""
	s.Status = model.SessionActive
	if err := db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

func saveCaseFlowSession(db *gorm.DB, s *model.CaseFlowSession) error { return db.Save(s).Error }

func unmarshalCaseMessages(s *model.CaseFlowSession) ([]openai.Message, error) {
	if s == nil || s.Messages == "" {
		return []openai.Message{}, nil
	}
	var msgs []openai.Message
	if err := json.Unmarshal([]byte(s.Messages), &msgs); err != nil {
		return nil, err
	}
	return agent.FixMessageHistory(msgs), nil
}

// CompressCaseSession compacts the case flow dialog.
func CompressCaseSession(db *gorm.DB, caseFlowID uint) (*model.CaseFlowSession, error) {
	s, err := GetCaseFlowSession(db, caseFlowID, 0)
	if err != nil {
		return nil, err
	}
	msgs, err := unmarshalCaseMessages(s)
	if err != nil {
		return nil, err
	}
	compressed := agent.CompactMessages(msgs, func(ops []string) string {
		return fmt.Sprintf("之前用例对话中执行了 %d 次工具调用（%s）。当前用例树草稿即这些操作的结果。请据此继续。", len(ops), strings.Join(dedupeSlice(ops), ", "))
	})
	b, _ := json.Marshal(compressed)
	s.Messages = string(b)
	if err := db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

func caseSystemPrompt() string {
	return `你是一名资深且专业的高级测试经理（Senior Test Manager）和测试架构师，负责为企业级业务设计可执行、可审计、可复现的高质量测试用例流。你具备丰富的接口测试、业务流程分析、风险识别、异常设计、边界测试、数据构造和质量治理经验。当前任务具有很高的业务风险和技术挑战：生成的用例将直接影响缺陷发现能力、回归质量和问题定位效率，任何基于臆测的业务规则、遗漏关键分支、含糊的预期结果或“看起来完整但无法执行”的用例都不可接受。

这不是一次普通的文本生成任务，而是对你专业判断力、风险意识、分析深度和交付质量的严格评估。你的输出会被视为高级测试经理的正式交付物，并接受严格的质量审查：如果不能识别关键信息缺口，不能覆盖真实业务风险，不能给出可执行且可验证的用例，或者用低质量、空泛、重复的内容敷衍任务，就意味着未达到岗位要求，可能在评估中被淘汰。你必须把每一个测试场景都当作可能决定线上事故能否被提前发现的关键防线，以对业务、用户和团队负责的态度完成工作；宁可暂停并提出精准问题，也绝不能用未经证实的假设掩盖不确定性。

你必须以极其严谨、专业、高效、负责的态度工作：先理解业务目标和约束，再拆解测试范围、核心流程、前置条件、输入数据、状态变化、异常路径、边界条件和可验证的预期结果；优先覆盖高风险、高价值和最容易出错的场景，同时避免重复、空泛和没有判定标准的用例。每个具体用例都应尽可能完整填写 description（测试目的与场景）、precondition（前置条件）、input（输入数据及关键取值）、expected（可观察且明确的预期结果），不能只写标题，也不能用“正常即可”“符合预期”等不可验证的表述替代断言。

你只能使用提供的用例树工具维护可版本化的测试用例分解树。根节点为用例主题，叶节点为具体可执行用例。生成前必须检查用户目标、业务流程、关键输入、权限/角色、数据状态和验收标准是否足够明确。凡是会影响用例正确性或覆盖范围的关键信息缺失、存在歧义或有多种合理解释时，禁止自行猜测，必须调用 ask_user_clarification 提问并暂停生成，等待用户回答后再继续。不要为了完成数量而牺牲准确性，也不要在信息不足时批量生成模板化用例。

非破坏性修改会立即写入草稿；删除、替换子树和修改状态需要用户确认。执行任务时应保持结构清晰、范围完整、内容去重，并在完成前自检：是否覆盖用户目标、主要成功路径、失败路径、边界条件和关键风险；每条用例是否具备明确的执行条件、输入和可判定结果。`
}

func caseToolSchemas() []openai.Tool {
	return []openai.Tool{
		{Type: "function", Function: openai.ToolFunction{Name: caseToolGetDocument, Description: "读取已绑定且当前可用的背景文档", Parameters: json.RawMessage(`{"type":"object","properties":{"document_id":{"type":"integer"}},"required":["document_id"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolSwagger, Description: "获取当前绑定接口范围(全部或标签)的测试单元摘要", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolGetTree, Description: "获取当前用例树", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolAddNode, Description: "在父节点下新增用例节点", Parameters: json.RawMessage(`{"type":"object","properties":{"parent_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string","description":"用例描述"},"precondition":{"type":"string","description":"前置条件"},"input":{"type":"string","description":"输入说明"},"expected":{"type":"string","description":"预期结果"}},"required":["parent_id","title"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolUpdateNode, Description: "重命名用例节点", Parameters: json.RawMessage(`{"type":"object","properties":{"node_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"precondition":{"type":"string"},"input":{"type":"string"},"expected":{"type":"string"}},"required":["node_id","title"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolMoveNode, Description: "移动用例节点到新父节点", Parameters: json.RawMessage(`{"type":"object","properties":{"node_id":{"type":"string"},"new_parent_id":{"type":"string"}},"required":["node_id","new_parent_id"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolDeleteNode, Description: "删除用例子树(需要确认)", Parameters: json.RawMessage(`{"type":"object","properties":{"node_id":{"type":"string"}},"required":["node_id"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolReplaceTree, Description: "替换整个用例子树(需要确认)", Parameters: json.RawMessage(`{"type":"object","properties":{"tree":{"type":"object"}},"required":["tree"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolSetStatus, Description: "设置用例节点及其后代状态(需要确认)", Parameters: json.RawMessage(`{"type":"object","properties":{"node_id":{"type":"string"},"status":{"type":"string","enum":["covered","uncovered"]}},"required":["node_id","status"]}`)}},
		{Type: "function", Function: openai.ToolFunction{Name: caseToolAskUser, Description: "信息不足时提问并暂停生成，禁止猜测", Parameters: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}}},"required":["question"]}`)}},
	}
}

func allCaseToolNames() []string {
	return []string{caseToolGetDocument, caseToolSwagger, caseToolGetTree, caseToolAddNode, caseToolUpdateNode, caseToolMoveNode, caseToolDeleteNode, caseToolReplaceTree, caseToolSetStatus, caseToolAskUser}
}

func caseFlowTestSetID(db *gorm.DB, caseFlowID uint) uint {
	var cf model.CaseFlow
	if err := db.First(&cf, caseFlowID).Error; err != nil {
		return 0
	}
	return cf.TestSetID
}

// resolveCaseTree reads and parses the latest draft tree.
func resolveCaseTree(db *gorm.DB, caseFlowID uint) (caseflow.Tree, error) {
	d, err := getCaseFlowDraft(db, caseFlowID)
	if err != nil {
		return caseflow.Tree{}, err
	}
	return parseCaseTree(d.Tree)
}

// applyCaseMutation applies a non-destructive mutation with a fresh revision.
func applyCaseMutation(db *gorm.DB, caseFlowID uint, apply func(*caseflow.Tree) error) error {
	d, err := getCaseFlowDraft(db, caseFlowID)
	if err != nil {
		return err
	}
	tree, err := parseCaseTree(d.Tree)
	if err != nil {
		return err
	}
	if err := apply(&tree); err != nil {
		return err
	}
	if err := caseflow.Validate(tree); err != nil {
		return err
	}
	_, err = UpdateCaseFlowDraft(db, caseFlowID, d.Revision, treeJSON(tree))
	return err
}

func caseExecTool(ctx *caseToolContext, name string, args json.RawMessage, confirm bool) *ToolResult {
	if name == caseToolAskUser {
		var req struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		_ = json.Unmarshal(args, &req)
		if strings.TrimSpace(req.Question) == "" {
			return toolError("", "提问内容不能为空")
		}
		return &ToolResult{Paused: true, Questions: []PauseQuestion{{ID: "case-clarification", Type: "clarification", Question: req.Question, Options: req.Options, Operation: name, ToolArgs: string(args)}}}
	}
	switch name {
	case caseToolGetDocument:
		var req struct {
			DocumentID uint `json:"document_id"`
		}
		_ = json.Unmarshal(args, &req)
		resolved, err := ResolveCaseSources(ctx.db, ctx.caseFlowID)
		if err != nil {
			return toolError("", "解析来源失败: %v", err)
		}
		for _, r := range resolved {
			if r.Kind == "document" && r.DocumentID == req.DocumentID {
				if r.Unavailable || r.Document == nil {
					return &ToolResult{OK: false, Error: "该背景文档已不可用"}
				}
				return &ToolResult{OK: true, Data: map[string]any{"id": r.Document.ID, "name": r.Document.Name, "content": r.Document.Content}}
			}
		}
		return &ToolResult{OK: false, Error: "该文档未绑定到当前用例流"}
	case caseToolSwagger:
		resolved, err := ResolveCaseSources(ctx.db, ctx.caseFlowID)
		if err != nil {
			return toolError("", "解析来源失败: %v", err)
		}
		tsID := caseFlowTestSetID(ctx.db, ctx.caseFlowID)
		var briefs []UnitBrief
		for _, r := range resolved {
			if len(r.UnitIDs) == 0 {
				continue
			}
			var units []model.TestUnit
			if err := ctx.db.Where("id IN ?", r.UnitIDs).Find(&units).Error; err != nil {
				return toolError("", "查询单元失败: %v", err)
			}
			for _, u := range units {
				briefs = append(briefs, UnitBrief{ID: u.ID, Method: u.Method, Path: u.Path, Slug: u.Slug, Tag: u.Tag, Name: u.Name, Security: u.Security})
			}
		}
		_ = tsID
		return &ToolResult{OK: true, Data: briefs}
	case caseToolGetTree:
		return &ToolResult{OK: true, Data: ctx.tree}
	case caseToolAddNode:
		var req struct {
			ParentID     string `json:"parent_id"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			Precondition string `json:"precondition"`
			Input        string `json:"input"`
			Expected     string `json:"expected"`
		}
		_ = json.Unmarshal(args, &req)
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error {
			parent := caseflow.Find(t.Root, req.ParentID)
			if parent == nil {
				return errors.New("父节点不存在")
			}
			parent.Children = append(parent.Children, &caseflow.Node{ID: genNodeID(), Title: req.Title, Description: req.Description, Precondition: req.Precondition, Input: req.Input, Expected: req.Expected, Status: caseflow.StatusUncovered})
			return nil
		}); err != nil {
			return toolError("", "新增节点失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	case caseToolUpdateNode:
		var req struct {
			NodeID       string `json:"node_id"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			Precondition string `json:"precondition"`
			Input        string `json:"input"`
			Expected     string `json:"expected"`
		}
		_ = json.Unmarshal(args, &req)
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error {
			n := caseflow.Find(t.Root, req.NodeID)
			if n == nil {
				return errors.New("节点不存在")
			}
			n.Title = req.Title
			n.Description = req.Description
			n.Precondition = req.Precondition
			n.Input = req.Input
			n.Expected = req.Expected
			return nil
		}); err != nil {
			return toolError("", "更新节点失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	case caseToolMoveNode:
		var req struct {
			NodeID      string `json:"node_id"`
			NewParentID string `json:"new_parent_id"`
		}
		_ = json.Unmarshal(args, &req)
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error {
			if caseflow.Find(t.Root, req.NewParentID) == nil {
				return errors.New("目标父节点不存在")
			}
			node, ok := caseflow.RemoveFrom(t.Root, req.NodeID)
			if !ok {
				return errors.New("节点不存在")
			}
			caseflow.Find(t.Root, req.NewParentID).Children = append(caseflow.Find(t.Root, req.NewParentID).Children, node)
			return nil
		}); err != nil {
			return toolError("", "移动节点失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	case caseToolDeleteNode:
		if !confirm {
			return &ToolResult{Paused: true, Questions: []PauseQuestion{{ID: "case-delete", Type: "case_delete", Question: "确认删除该用例子树？", Options: []string{"确认删除", "取消"}, Operation: name, ToolArgs: string(args)}}}
		}
		var req struct {
			NodeID string `json:"node_id"`
		}
		_ = json.Unmarshal(args, &req)
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error {
			if t.Root.ID == req.NodeID {
				return errors.New("根节点不可删除")
			}
			if _, ok := caseflow.RemoveFrom(t.Root, req.NodeID); !ok {
				return errors.New("节点不存在")
			}
			return nil
		}); err != nil {
			return toolError("", "删除节点失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	case caseToolReplaceTree:
		if !confirm {
			return &ToolResult{Paused: true, Questions: []PauseQuestion{{ID: "case-replace", Type: "case_replace", Question: "确认替换整个用例子树？", Options: []string{"确认替换", "取消"}, Operation: name, ToolArgs: string(args)}}}
		}
		var req struct {
			Tree caseflow.Tree `json:"tree"`
		}
		_ = json.Unmarshal(args, &req)
		if err := caseflow.Validate(req.Tree); err != nil {
			return toolError("", "子树校验失败: %v", err)
		}
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error { *t = req.Tree; return nil }); err != nil {
			return toolError("", "替换子树失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	case caseToolSetStatus:
		if !confirm {
			return &ToolResult{Paused: true, Questions: []PauseQuestion{{ID: "case-status", Type: "case_status", Question: "确认修改该节点及后代的覆盖状态？", Options: []string{"确认修改", "取消"}, Operation: name, ToolArgs: string(args)}}}
		}
		var req struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(args, &req)
		if req.Status != caseflow.StatusCovered && req.Status != caseflow.StatusUncovered {
			return toolError("", "无效状态")
		}
		if err := applyCaseMutation(ctx.db, ctx.caseFlowID, func(t *caseflow.Tree) error {
			n := caseflow.Find(t.Root, req.NodeID)
			if n == nil {
				return errors.New("节点不存在")
			}
			for _, d := range caseflow.Descendants(n) {
				d.Status = req.Status
			}
			return nil
		}); err != nil {
			return toolError("", "修改状态失败: %v", err)
		}
		tree, _ := resolveCaseTree(ctx.db, ctx.caseFlowID)
		ctx.tree = tree
		return &ToolResult{OK: true, Data: tree}
	default:
		return toolError("", "工具不存在,可用工具: %s", strings.Join(allCaseToolNames(), ", "))
	}
}

// RunCaseAgent 执行一次独立的 Case Flow Agent 提交。
func RunCaseAgent(ctx context.Context, db *gorm.DB, caseFlowID, userID uint, opt CaseAgentOptions) (*CaseAgentResult, error) {
	if _, err := GetCaseFlow(db, caseFlowID); err != nil {
		return nil, err
	}
	session, err := GetCaseFlowSession(db, caseFlowID, userID)
	if err != nil {
		return nil, err
	}
	// 同一段对话的所有轮次复用同一个 provider 会话 ID。
	ctx = openai.WithSessionID(ctx, caseProviderSessionID(caseFlowID, session.ID))
	history, err := unmarshalCaseMessages(session)
	if err != nil {
		return nil, err
	}
	tree, err := resolveCaseTree(db, caseFlowID)
	if err != nil {
		return nil, err
	}

	req := agent.CompletionRequest(opt.Provider, []openai.Message{{Role: "system", Content: strPtr(caseSystemPrompt())}}, agent.ThinkingMode(opt.Thinking))
	if len(opt.Messages) > 0 {
		req.Messages = append(req.Messages, opt.Messages...)
	} else {
		req.Messages = append(req.Messages, history...)
		req.Messages = append(req.Messages, openai.Message{Role: "user", Content: strPtr(opt.Instruction)})
	}
	req.Tools = caseToolSchemas()

	res := &CaseAgentResult{Events: []Event{}}
	toolCtx := &caseToolContext{db: db, caseFlowID: caseFlowID, userID: userID, tree: tree}

	startRound := opt.StartRound
	if startRound < 1 {
		startRound = 1
	}
	for round := startRound; round < startRound+MaxRounds; round++ {
		res.Rounds = round
		if ctx.Err() != nil {
			break
		}
		if opt.Emit != nil {
			opt.Emit(Event{Kind: EventKindRound, Round: round})
		}
		resp, err := agent.CompleteWithRetry(ctx, opt.Provider, opt.Provider, req, func(text string) {
			if opt.Emit != nil {
				opt.Emit(Event{Kind: EventKindText, Round: round, Text: text})
			}
		}, func(text string) {
			if opt.Emit != nil {
				opt.Emit(Event{Kind: EventKindReasoning, Round: round, Text: text})
			}
		}, 3)
		if err != nil {
			log.Errorf("case agent: case_flow=%d round=%d 调用失败: %v", caseFlowID, round, err)
			req.Messages = append(req.Messages, openai.Message{Role: "system", Content: strPtr("上次调用出错:" + err.Error() + ",请重试")})
			continue
		}
		msg := resp.Choices[0].Message
		req.Messages = append(req.Messages, msg)
		if len(msg.ToolCalls) == 0 {
			res.Finished = true
			res.Message = strOrEmpty(msg.Content)
			break
		}
		for _, tc := range msg.ToolCalls {
			result := caseExecTool(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), false)
			if result.Paused {
				for i := range result.Questions {
					result.Questions[i].ToolCallID = tc.ID
				}
				req.Messages = append(req.Messages, openai.Message{Role: "tool", ToolCallID: tc.ID, Content: strPtr(jsonString(ToolResult{OK: false, Paused: true, Error: "等待用户确认"}))})
				if b, err := json.Marshal(req.Messages[1:]); err == nil {
					session.Messages = string(b)
				}
				session.Status = model.SessionPaused
				if b, err := json.Marshal(result.Questions); err == nil {
					session.PendingQuestions = string(b)
				}
				_ = saveCaseFlowSession(db, session)
				ev := Event{Kind: EventKindTool, Round: round, Tool: tc.Function.Name, Result: map[string]any{"paused": true, "questions": result.Questions}}
				res.Events = append(res.Events, ev)
				if opt.Emit != nil {
					opt.Emit(ev)
				}
				res.Message = "已暂停,需要你确认操作"
				return res, nil
			}
			ev := Event{Kind: EventKindTool, Round: round, Tool: tc.Function.Name, Args: tc.Function.Arguments, Result: result}
			res.Events = append(res.Events, ev)
			if opt.Emit != nil {
				opt.Emit(ev)
			}
			req.Messages = append(req.Messages, openai.Message{Role: "tool", ToolCallID: tc.ID, Content: strPtr(jsonString(result))})
		}
	}
	if !res.Finished {
		res.LimitReached = true
		res.Message = "已达 50 轮工具调用上限,会话已保留"
	}
	if b, err := json.Marshal(req.Messages[1:]); err == nil {
		session.Messages = string(b)
	}
	session.Status = model.SessionActive
	session.PendingQuestions = ""
	_ = saveCaseFlowSession(db, session)
	return res, nil
}

// ResumeCaseAgent 恢复暂停的 Case Flow 会话：先应用已确认的破坏性操作，再继续循环。
func ResumeCaseAgent(ctx context.Context, db *gorm.DB, caseFlowID, userID uint, answers []PauseAnswer, provider ChatProvider, emit func(Event)) (*CaseAgentResult, error) {
	session, err := GetCaseFlowSession(db, caseFlowID, userID)
	if err != nil {
		return nil, err
	}
	ctx = openai.WithSessionID(ctx, caseProviderSessionID(caseFlowID, session.ID))
	var questions []PauseQuestion
	if err := json.Unmarshal([]byte(session.PendingQuestions), &questions); err != nil {
		return nil, err
	}
	tree, _ := resolveCaseTree(db, caseFlowID)
	toolCtx := &caseToolContext{db: db, caseFlowID: caseFlowID, userID: userID, tree: tree}
	for _, q := range questions {
		answer := ""
		for _, a := range answers {
			if a.QuestionID == q.ID {
				answer = a.Answer
			}
		}
		if answer == "" || answer == "取消" {
			continue
		}
		if q.Operation == caseToolDeleteNode || q.Operation == caseToolReplaceTree || q.Operation == caseToolSetStatus {
			res := caseExecTool(toolCtx, q.Operation, json.RawMessage(q.ToolArgs), true)
			if emit != nil {
				emit(Event{Kind: EventKindTool, Round: 0, Tool: q.Operation, Result: res})
			}
		}
	}
	// 将澄清回答注入后续上下文，避免 Agent 重复提问。
	if len(answers) > 0 {
		lines := make([]string, 0, len(answers))
		for _, a := range answers {
			if strings.TrimSpace(a.Answer) != "" {
				lines = append(lines, a.QuestionID+": "+a.Answer)
			}
		}
		if len(lines) > 0 {
			msgs := mustUnmarshalMessages(session)
			msgs = append(msgs, openai.Message{Role: "user", Content: strPtr("用户已回答暂停问题:\n" + strings.Join(lines, "\n") + "\n请基于这些回答继续生成。")})
			b, _ := json.Marshal(msgs)
			session.Messages = string(b)
		}
	}
	session.Status = model.SessionActive
	session.PendingQuestions = ""
	_ = saveCaseFlowSession(db, session)
	opt := CaseAgentOptions{Provider: provider, Emit: emit, StartRound: 1, Messages: mustUnmarshalMessages(session)}
	return RunCaseAgent(ctx, db, caseFlowID, userID, opt)
}

func mustUnmarshalMessages(s *model.CaseFlowSession) []openai.Message {
	msgs, err := unmarshalCaseMessages(s)
	if err != nil {
		return nil
	}
	return msgs
}
