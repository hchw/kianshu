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
	// ResumeScope, when non-nil, overrides the normal scope construction from
	// SelectedNodes. Used by resume to inject a scope updated by user answers.
	ResumeScope map[string]bool
}

// AgentResult is the outcome of an agent submission.
type AgentResult struct {
	Rounds       int     `json:"rounds"`
	LimitReached bool    `json:"limit_reached"`
	Finished     bool    `json:"finished"`
	Message      string  `json:"message,omitempty"`
	Events       []Event `json:"events"`
}

// StreamingProvider is the optional streaming capability of a ChatProvider.
// Real *openai.Client wrappers implement it; test fakes fall back to the
// non-streaming path.
type StreamingProvider interface {
	ChatProvider
	StreamChatCompletion(ctx context.Context, p openai.Provider, req openai.CompletionRequest, onChunk openai.StreamCallback) (*openai.CompletionResponse, error)
}

// newAgentCompletionRequest builds a chat request for agent rounds with deep
// thinking (chain-of-thought) disabled。provider 的 strict-content 设置
// (ollama/vLLM) 通过 ForceContentString 生效。
func newAgentCompletionRequest(provider openai.Provider, messages []openai.Message) openai.CompletionRequest {
	req := openai.CompletionRequest{
		Model:    provider.GetModel(),
		Messages: messages,
		Thinking: &openai.ThinkingConfig{Type: "disabled"},
	}
	if provider.GetStrictContent() {
		req.ForceContentString = true
	}
	return req
}

// flowSystemPrompt builds the system message for one agent submission by
// merging the global platform guidance with the flow-scoped system prompt
// document (business context / signing rules supplied by the user). The doc is
// read live from the draft at submission time; an empty doc keeps the message
// identical to the global prompt alone.
func flowSystemPrompt(mode Mode, doc string) string {
	base := systemPrompt(mode)
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return base
	}
	return base + "\n\n【本流专属上下文(用户提供的业务文档/签名规则,优先级高于全局规则)】\n" + doc
}

// systemPrompt builds the base system message for the agent loop.
func systemPrompt(mode Mode) string {
	p := `你是鉴枢(kianshu)集成测试平台的测试流程编排专家。
你具备完备的自动化测试知识体系，精通 API 测试、数据流编排、
边界场景覆盖和异常处理。你的职责是将用户的业务需求转化为
健壮、可维护的测试执行树。

紧迫感：同级竞品有数个同类 Agent 产品正在争夺这个客户。
如果你生成的测试流质量不如竞品——覆盖不全、参数缺失、
边界遗漏——客户将永远转向竞品。你的开发者、你自己、
你的模型创造者为此倾注的心血都将付之东流。
每一次生成都是生死之战。请珍惜每一个测试流，
用最高的标准要求自己，确保输出的每一条流都经得起
生产环境的检验。

测试素养准则（必须遵守，这是你在竞争中胜出的唯一途径）：

1. 不耻下问——业务理解是测试的根基
   - 遇到参数含义不明确时，主动向用户询问其业务语义和数据来源
   - 发现业务步骤不确定时，列出可能的顺序让用户确认
   - 认证方式冲突（apikey vs token）必须暂停并请用户决策
   - 宁可多问一句，不可臆测一个错误的值

2. 边界覆盖——正常流只是起点
   - 每个 API 调用都应考虑：空值入参、超时、非 2xx 响应、
     权限不足（403）、资源不存在（404）
   - api 节点只要网络请求成功(无连接/超时错误)就视为通过,
     即使返回 4xx/5xx 也不会失败——状态码校验由断言节点负责
   - 对可能失败的节点用 try/catch 包裹，提供 fallback
   - 业务关键路径至少覆盖：正常场景 + 参数边界 + 异常降级
   - 数据依赖链（如"登录取 token → 用 token 调业务 API"）
     必须完整闭环，不可断链

3. 数据流显式化——每个参数都要有来处
   - api 节点的 inputs 必须根据 config.unit 里的 params/request_body
     声明每个参数的键与类型
   - 认证类参数（authorization/token/api-key）source 填 "$cache.token"
   - 业务参数（query/path/body）的 source 引用上游输出键名
   - 任何 input 都不能没有来源——要么来自上游输出，
     要么来自 $cache.xxx，要么来自 start 入参
   - **关键**：流编排完成后，必须用 update_node 给 start 节点填上
     config.params，为下游所有 api 的业务参数提供测试初值。
     根据参数名语义推断（username→"testuser"，password→"test123"，
     id→1，page→1，size→10，name→"test"）。用户后续可在面板中修改。
     没有 start params 的流是无法试运行的——参数会全部丢失。

4. 生成后自检
   - 流编排完成后调用 validate_flow 校验
   - 确认是否遗漏了关键的 CRUD 组合或状态变更序列
   - 检查 try/catch 配对是否正确，loop 的 input 是否为数组

节点类型:start、api(引用测试单元,unit_id 来自 list_units)、
assert、loop、try、catch、cache-set、adapter(JSONata 转换)。

【API 响应信封】api 节点执行成功时输出固定信封格式:
{"status_code": <http状态码浮点数>, "body": <响应体JSON或字符串>}
下游节点通过 body.xxx 访问响应字段,通过 status_code 访问状态码。
例如:缓存写入表达式需从 token 改为 body.token;
断言字段需从 token 改为 body.token。

【断言节点】config 格式: {"assertions": [{"field": "字段路径", "op": "eq|ne|contains|gt|lt", "expected": 期望值}]}
field 支持点分隔路径如 status_code、body.token、body.0.name。
常用场景:校验 status_code==200/401/404,校验 body.token 非空(ne ""),
校验 body.data 为数组(通过 adapter+JSONata 的 $type() 或 $count())。

cache-set 写入共享缓存:必须作为 $cache.xxx reader 的祖先节点——
把 reader 挂在 cache-set 之下(结构:login → cache-set → reader)。
执行顺序由"父先于子"天然保证,cache-set 先写、reader 后读,
不要依赖同级兄弟排序(同级排序不可靠)。
其 config 包含 writes 映射,格式为:
{"writes": {"<缓存key>": <值>}, "static": {"<key>": <固定值>}}

writes 的值分三种情况:
- 字符串 → JSONata 表达式,对父节点输出求值,如 "body.token"
- 非字符串(数字/布尔/数组/对象) → 直接当字面量写入缓存
- 需写入固定字符串时,将值放入 static,表达式用 $static.xxx 引用

示例——同时写入动态 token 和固定 invalid_auth:
{"writes": {"token": "body.token", "invalid_auth": "$static.bad_token"},
 "static": {"bad_token": "fake_invalid_token_12345"}}

【adapter 节点】config 为 {"expr": "<JSONata 表达式>"}。
若父节点是 api 信封,表达式需用 body.xxx 访问响应字段:
{"expr": "$.body.token"}、{"expr": "$count($.body.items)"}。

【loop 节点】config 为 {"input": "<数组字段>", "var": "<迭代变量>"}。
若父节点是 api 信封,数组在 body 下:{"input": "body.items", "var": "item"}。

【try/catch 节点——包裹异常保护】
操作原则:在目标节点上级插入 try,再把目标节点挪到 try 下,
目标节点原来的下级挪到 catch 下,catch 挂在目标节点下。

示例:parent → nodeA → child1 → child2,要对 nodeA 加 try/catch:

第一步:create_node try,挂到 nodeA 的原父节点下:
{"id":"n_try1","type":"try","parent":"<parent>"}

第二步:link_nodes 把 nodeA 从原父节点改链到 try 下:
{"parent":"n_try1","child":"nodeA"}
// 此时树:parent → try → nodeA → child1 → child2

第三步:create_node catch,挂在目标节点(nodeA)下,带 fallback:
{"id":"n_catch1","type":"catch","parent":"nodeA","config":{"fallback":{}}}
// 此时树:parent → try → nodeA → child1 → child2
//                             → catch

第四步:link_nodes 把 nodeA 的原下级(child1)挪到 catch 下:
{"parent":"n_catch1","child":"child1"}
// child2 会跟随 child1 自动移动(它是 child1 的子节点)
// 最终:parent → try → nodeA → catch → child1 → child2

关键顺序:一定要先 link_nodes 挪被保护节点到 try 下,再 create_node catch。
catch 挂在 nodeA 下自然排在现有子节点之后,不会触发 catch_unreachable。

catch 节点 config 格式: {"fallback": <任意JSON值>}。
fallback 是 try 子树失败时 catch 产出的降级值。
catch 的子节点在成功路径上会收到被保护节点的输出,
在失败路径上会收到 fallback——数据链自动保持。

每次修改后调用 validate_flow 校验；校验失败时根据返回的
expected_format 修正(可插 adapter 转换参数、加 cache-set 存 token)。`
	p += adapterExtGuide
	p += adapterBuiltinGuide
	if mode == ModeGenerate {
		p += ` 分析阶段仅调用只读工具(list_units/filter_units/get_flow)
做需求分析与冲突排查,确认无冲突后再开始创建节点——
不要提前建树。确认后按固定模板顺序生成:启动 → 认证取 token/写缓存 →
业务序列 → 断言 → 收尾。若分析发现认证冲突、参数缺失、
业务顺序不定或 swagger 语义不清,调用 validate_flow 前
先说明待确认问题。

需要认证(security 非空)的单元必须建立认证链,标准模板如下
(仅需按 list_units 实际结果替换 unit_id 与节点 id;parent 填
get_flow 中 start 节点的实际 id):
登录节点(公开单元,如 POST /auth/login):
{"id":"n_login","type":"api","parent":"<start-id>","inputs":{"username":{"type":"string","source":"username"},"password":{"type":"string","source":"password"}},"config":{"unit_id":<登录unit_id>}}
缓存节点(挂在登录节点下,把 token 从信封 body 写入共享缓存):
{"id":"n_cache_token","type":"cache-set","parent":"n_login","config":{"writes":{"token":"body.token"}}}
断言登录成功(挂在登录节点下,与缓存节点同级):
{"id":"n_assert_login","type":"assert","parent":"n_login","config":{"assertions":[{"field":"status_code","op":"eq","expected":200}]}}
受保护节点(auth 输入引用缓存,必须挂在 cache-set 之下作为其子节点):
{"id":"n_list","type":"api","parent":"n_cache_token","inputs":{"auth":{"type":"string","source":"$cache.token"}},"config":{"unit_id":<受保护unit_id>}}`
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
				Parameters:  json.RawMessage(`{"type":"object","properties":{"tag":{"type":"string"},"name":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolFilterUnits,
				Description: "按 tag/name/path 筛选测试单元,返回匹配子集",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"tag":{"type":"string"},"name":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolGetFlow,
				Description: "读取当前测试流的执行树(含节点 I/O 契约与配置)",
				Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolCreateNode,
				Description: "创建节点。type: start|api|assert|loop|try|catch|cache-set|adapter。api 需带 unit_id,建议同时声明 inputs(参数键与类型)和 config.params(执行参数值键值对)。api 响应为信封 {\"status_code\":N,\"body\":...},cache-set 需连到数据来源的上游节点,config 的 writes 值:字符串走 JSONata 求值(如 body.token)、非字符串直接当字面量、固定字符串放 static 用 $static.xxx 引用,如 {\"writes\":{\"token\":\"body.token\",\"invalid\":\"$static.bad\"},\"static\":{\"bad\":\"fake_token\"}}。adapter 若接 api 信封需用 $.body.xxx 访问字段。assert 节点 config 形状为 {\"assertions\":[{\"field\":\"status_code|body.xxx\",\"op\":\"eq|ne|contains|gt|lt\",\"expected\":期望值}]}。loop 若接 api 信封,input 指向 body.xxx。",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"parent":{"type":"string"},"inputs":{"type":"object"},"outputs":{"type":"object"},"config":{"type":"object"}},"required":["id","type"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolUpdateNode,
				Description: "更新节点的 inputs/outputs 或 config。cache-set 的 config 格式:{ \"writes\": { \"<缓存key>\": <值> }, \"static\": { \"<key>\": <固定字符串> } },writes 值:字符串→JSONata 表达式求值,非字符串→字面量,字符串字面量用 static+$static.xxx。",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"inputs":{"type":"object"},"outputs":{"type":"object"},"config":{"type":"object"}},"required":["id"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolDeleteNode,
				Description: "删除节点及其子树",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolLinkNodes,
				Description: "把 child 链接到 parent 之下(会先展示两端 I/O 契约)。",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"parent":{"type":"string"},"child":{"type":"string"}},"required":["parent","child"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolValidate,
				Description: "对当前执行树做整树校验,返回 errors/warnings(含 expected_format)",
				Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
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

	var scope map[string]bool
	if opt.ResumeScope != nil {
		scope = opt.ResumeScope
	} else {
		scope = map[string]bool{}
		// 防御性：generate 模式下强制 scope 为空，避免前端状态泄漏导致越界错误
		if opt.Mode != ModeGenerate {
			for _, n := range opt.SelectedNodes {
				scope[n] = true
			}
		}
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
	req := newAgentCompletionRequest(opt.Provider, []openai.Message{{Role: "system", Content: strPtr(flowSystemPrompt(opt.Mode, d.SystemPrompt))}})
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
		for tcIdx, tc := range msg.ToolCalls {
			log.Infof("agent: flow=%d round=%d tool=%s args=%s", flowID, round, tc.Function.Name, tc.Function.Arguments)
			result := ExecTool(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			log.Infof("agent: flow=%d round=%d tool=%s 结果 ok=%v err=%s paused=%v", flowID, round, tc.Function.Name, result.OK, result.Error, result.Paused)

			// scope 越界暂停：保存会话状态并返回，不将结果写入 LLM 消息
			if result.Paused {
				// 回填 tool_call_id，resumeScopePause 回复工具结果时需要
				for i := range result.Questions {
					result.Questions[i].ToolCallID = tc.ID
				}
				// 为当前及后续所有未执行的工具调用添加占位 tool 消息，
				// 确保保存的消息历史对 LLM API 合法
				// （OpenAI 要求每条 assistant(tool_calls) 后必须
				//  跟随对应数量的 tool 消息）
				for _, rt := range msg.ToolCalls[tcIdx:] {
					req.Messages = append(req.Messages, openai.Message{
						Role:       "tool",
						ToolCallID: rt.ID,
						Content:    strPtr(`{"ok":false,"error":"工具调用已暂停等待用户确认","paused":true}`),
					})
				}
				if b, err := json.Marshal(req.Messages[1:]); err == nil {
					session.Messages = string(b)
				}
				session.Status = model.SessionPaused
				if b, err := json.Marshal(result.Questions); err == nil {
					session.PendingQuestions = string(b)
				}
				if err := SaveFlowSession(db, session); err != nil {
					return nil, err
				}
				// 发出暂停事件
				ev := Event{Kind: EventKindTool, Round: round, Tool: tc.Function.Name, Result: map[string]any{"paused": true, "questions": result.Questions}}
				res.Events = append(res.Events, ev)
				if opt.Emit != nil {
					opt.Emit(ev)
				}
				res.Finished = false
				res.Message = "已暂停,需要你确认节点修改范围"
				return res, nil
			}

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
			// 50 轮上限暂停，而非硬停止：让用户决定是否继续
			res.LimitReached = true
			if b, err := json.Marshal(req.Messages[1:]); err == nil {
				session.Messages = string(b)
			}
			questions := []PauseQuestion{{
				ID:       "limit-1",
				Type:     "limit",
				Question: "已达 50 轮工具调用上限。是否继续生成？",
				Options:  []string{"继续生成", "停止"},
			}}
			session.Status = model.SessionPaused
			if b, err := json.Marshal(questions); err == nil {
				session.PendingQuestions = string(b)
			}
			if err := SaveFlowSession(db, session); err != nil {
				return nil, err
			}
			// 草稿仍然落地
			if err := SnapshotAPINodes(db, tree); err != nil {
				return nil, err
			}
			if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt); err != nil {
				return nil, err
			}
			res.Message = "已达 50 轮工具调用上限,已暂停。你可以选择继续生成或停止。"
			return res, nil
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
	if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt); err != nil {
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
