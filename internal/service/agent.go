package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ChatProvider is what the agent loop needs from an LLM: the provider config
// plus a chat-completion call. *openai.Client satisfies it; tests inject a fake.
type ChatProvider interface {
	openai.Provider
	ChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest) (*openai.CompletionResponse, error)
}

// MaxRounds is the per-submission tool-call round limit.
const MaxRounds = 50

// Mode selects the agent loop behavior.
type Mode string

const (
	ModeEdit     Mode = "edit"
	ModeGenerate Mode = "generate"
)

// Event kinds pushed over SSE in real time.
const (
	// EventKindRound announces that a new LLM round is starting.
	EventKindRound = "round"
	// EventKindText carries one incremental chunk of the assistant's reply.
	EventKindText = "text"
	// EventKindTool carries one tool invocation and its result (default kind).
	EventKindTool = "tool"
)

// Event is one round of LLM activity pushed in real time over SSE.
type Event struct {
	Kind   string `json:"kind,omitempty"` // "round" | "text" | "tool"(默认)
	Round  int    `json:"round"`
	Tool   string `json:"tool,omitempty"`
	Args   any    `json:"args,omitempty"`
	Result any    `json:"result,omitempty"`
	// Text is the incremental assistant content for kind=text events.
	Text string `json:"text,omitempty"`
}

// AgentHooks carries the real-time progress callbacks of an agent run
// (the SSE emit channel).
type AgentHooks struct {
	Emit func(Event)
}

func (h AgentHooks) emit(ev Event) {
	if h.Emit != nil {
		h.Emit(ev)
	}
}

// AgentOptions carries the inputs of one agent submission.
type AgentOptions struct {
	Provider      ChatProvider
	Instruction   string
	SelectedNodes []string
	Mode          Mode
	Emit          func(Event)
	// StartRound, when > 0, numbers the first round of this loop from that
	// value (used by generation so build rounds continue the analysis count).
	StartRound int
	// PresetMessages, when non-empty, replaces the system+history+user message
	// construction: the caller supplies the complete message list (history
	// only, system excluded). Used by the generation workflow to resume.
	PresetMessages []openai.Message
}

// AgentResult is the outcome of an agent submission.
type AgentResult struct {
	Rounds       int      `json:"rounds"`
	LimitReached bool     `json:"limit_reached"`
	Finished     bool     `json:"finished"`
	Message      string   `json:"message,omitempty"`
	Events       []Event  `json:"events"`
}

// StreamingProvider is the optional streaming capability of a ChatProvider.
// Real *openai.Client wrappers implement it; test fakes fall back to the
// non-streaming path.
type StreamingProvider interface {
	ChatProvider
	StreamChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest, onChunk openai.StreamCallback) (*openai.CompletionResponse, error)
}

// newAgentCompletionRequest builds a chat request for agent rounds with deep
// thinking (chain-of-thought) disabled.
func newAgentCompletionRequest(model string, messages []openai.Message) openai.CompletionRequest {
	return openai.CompletionRequest{
		Model:    model,
		Messages: messages,
		Thinking: &openai.ThinkingConfig{Type: "disabled"},
	}
}

// systemPrompt builds the base system message for the agent loop.
func systemPrompt(mode Mode) string {
	p := `你是鉴枢(kianshu)集成测试平台的测试流生成助手。你可以通过工具读取测试单元、编辑测试流的执行树并校验。执行树是单根树:一个 start 根节点,其余节点有唯一父节点;cache-set 节点不参与树连线(旁路)。` +
		`节点类型:start、api(引用测试单元,unit_id 来自 list_units)、assert、loop、try、catch、cache-set、adapter(JSONata 转换)。` +
		`每次修改后调用 validate_flow 校验;校验失败时根据返回的 expected_format 修正(可插 adapter 转换参数、加 cache-set 存 token)。`
	if mode == ModeGenerate {
		p += ` 按固定模板顺序生成:启动 → 认证取 token/写缓存 → 业务序列 → 断言 → 收尾。若分析发现认证冲突、参数缺失、业务顺序不定或 swagger 语义不清,调用 validate_flow 前先说明待确认问题。`
	}
	return p
}

// toolSchemas returns the tool definitions exposed to the LLM.
func toolSchemas() []openai.Tool {
	return []openai.Tool{
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolListUnits,
				Description: "列出测试集的全部测试单元摘要(method/path/tag),供生成流时引用 unit_id",
				Parameters: json.RawMessage(`{"type":"object","properties":{"tag":{"type":"string"},"name":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolFilterUnits,
				Description: "按 tag/name/path 筛选测试单元,返回匹配子集",
				Parameters: json.RawMessage(`{"type":"object","properties":{"tag":{"type":"string"},"name":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolGetFlow,
				Description: "读取当前测试流的执行树(含节点 I/O 契约与配置)",
				Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolCreateNode,
				Description: "创建节点。type: start|api|assert|loop|try|catch|cache-set|adapter。cache-set 不连线;api 需带 unit_id。",
				Parameters: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"parent":{"type":"string"},"inputs":{"type":"object"},"outputs":{"type":"object"},"config":{"type":"object"}},"required":["id","type"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolUpdateNode,
				Description: "更新节点的 inputs/outputs 或 config",
				Parameters: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"inputs":{"type":"object"},"outputs":{"type":"object"},"config":{"type":"object"}},"required":["id"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolDeleteNode,
				Description: "删除节点及其子树",
				Parameters: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolLinkNodes,
				Description: "把 child 链接到 parent 之下(会先展示两端 I/O 契约)。cache-set 不可连线。",
				Parameters: json.RawMessage(`{"type":"object","properties":{"parent":{"type":"string"},"child":{"type":"string"}},"required":["parent","child"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolValidate,
				Description: "对当前执行树做整树校验,返回 errors/warnings(含 expected_format)",
				Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
		},
	}
}

// RunAgent executes the tool-calling loop for one submission. The working tree
// is parsed from the flow draft, mutated by tools, and saved back via OnDraft.
func RunAgent(ctx context.Context, db *gorm.DB, flowID, userID uint, opt AgentOptions) (*AgentResult, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	tsID := flowTestSetID(db, flowID)

	scope := map[string]bool{}
	for _, n := range opt.SelectedNodes {
		scope[n] = true
	}

	toolCtx := &ToolContext{DB: db, TestSetID: tsID, Tree: tree, Scope: scope}

	session, err := GetFlowSession(db, flowID, userID)
	if err != nil {
		return nil, err
	}

	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}

	// Rebuild the message list for this submission: system + prior history + user.
	req := newAgentCompletionRequest(opt.Provider.GetModel(), []openai.Message{{Role: "system", Content: strPtr(systemPrompt(opt.Mode))}})
	if len(opt.PresetMessages) > 0 {
		req.Messages = append(req.Messages, opt.PresetMessages...)
	} else {
		req.Messages = append(req.Messages, history...)
		userMsg := opt.Instruction
		if opt.Mode == ModeGenerate {
			userMsg = "生成测试流:" + opt.Instruction
		}
		req.Messages = append(req.Messages, openai.Message{Role: "user", Content: strPtr(userMsg)})
	}
	req.Tools = toolSchemas()

	res := &AgentResult{Events: []Event{}}
	var lastText string

	startRound := opt.StartRound
	if startRound < 1 {
		startRound = 1
	}
	for round := startRound; round < startRound+MaxRounds; round++ {
		res.Rounds = round
		if ctx.Err() != nil {
			break
		}
		log.Infof("agent: flow=%d round=%d 调用 LLM (model=%s, 消息数=%d)", flowID, round, opt.Provider.GetModel(), len(req.Messages))
		if opt.Emit != nil {
			opt.Emit(Event{Kind: EventKindRound, Round: round})
		}

		// 3 次重试应对瞬态失败(网络抖动/限流)
		var resp *openai.CompletionResponse
		var lastErr error
		for retry := 0; retry < 3; retry++ {
			resp, lastErr = opt.complete(ctx, opt.Provider, req, func(text string) {
				if opt.Emit != nil {
					opt.Emit(Event{Kind: EventKindText, Round: round, Text: text})
				}
			})
			if lastErr == nil {
				break
			}
			if ctx.Err() != nil {
				break
			}
			if retry < 2 {
				log.Warnf("agent: flow=%d round=%d LLM 调用失败(第%d次重试): %v", flowID, round, retry+1, lastErr)
				time.Sleep(time.Duration(retry+1) * 2 * time.Second)
			}
		}
		if lastErr != nil {
			log.Errorf("agent: flow=%d round=%d LLM 调用失败(已重试3次): %v", flowID, round, lastErr)
			req.Messages = append(req.Messages, openai.Message{
				Role: "system", Content: strPtr("上次调用出错:" + lastErr.Error() + ",请按标准工具格式重试"),
			})
			continue
		}
		msg := resp.Choices[0].Message
		req.Messages = append(req.Messages, msg)

		if len(msg.ToolCalls) == 0 {
			lastText = strOrEmpty(msg.Content)
			res.Finished = true
			log.Infof("agent: flow=%d round=%d LLM 未发起工具调用, 完成: %s", flowID, round, lastText)
			break
		}
		log.Infof("agent: flow=%d round=%d LLM 发起 %d 个工具调用", flowID, round, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			log.Infof("agent: flow=%d round=%d tool=%s args=%s", flowID, round, tc.Function.Name, tc.Function.Arguments)
			result := ExecTool(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			log.Infof("agent: flow=%d round=%d tool=%s 结果 ok=%v err=%s", flowID, round, tc.Function.Name, result.OK, result.Error)
			ev := Event{Kind: EventKindTool, Round: round, Tool: tc.Function.Name, Args: tc.Function.Arguments, Result: result}
			res.Events = append(res.Events, ev)
			if opt.Emit != nil {
				opt.Emit(ev)
			}
			req.Messages = append(req.Messages, openai.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    strPtr(jsonString(result)),
			})
		}
	}

	if !res.Finished {
		if ctx.Err() != nil {
			res.Message = "连接已断开,生成已终止"
		} else {
			res.LimitReached = true
			res.Message = "已达 50 轮工具调用上限,已停止。你可以继续提交微调。"
		}
	}

	res.Message = firstNonEmpty(res.Message, lastText)

	// Persist the running messages (without this submission's system prefix
	// duplication) as the session history.
	if msgsJSON, err := json.Marshal(req.Messages[1:]); err == nil {
		session.Messages = string(msgsJSON)
	}
	session.Status = model.SessionActive
	if err := SaveFlowSession(db, session); err != nil {
		return nil, err
	}

	// Land the working tree back into the draft.
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, err
	}
	if _, err := UpdateDraft(db, flowID, d.Name, tree.String()); err != nil {
		return nil, err
	}
	return res, nil
}

// complete calls the provider, preferring streaming when available so the
// assistant's reply reaches the client token by token via onText.
func (opt AgentOptions) complete(ctx context.Context, p ChatProvider, req openai.CompletionRequest, onText func(string)) (*openai.CompletionResponse, error) {
	if sp, ok := p.(StreamingProvider); ok {
		return sp.StreamChatCompletion(ctx, p, req, onText)
	}
	return p.ChatCompletion(ctx, p, req)
}

func strPtr(s string) *string { return &s }

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
