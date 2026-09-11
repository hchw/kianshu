package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/agent"
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

// StreamingProvider 保留旧服务层类型名，兼容生成流程现有的类型断言；
// 实现委托给通用 Agent Runtime 的 StreamingChatClient。
type StreamingProvider = agent.StreamingChatClient

// MaxRounds is the per-submission tool-call round limit.
const MaxRounds = 50

// Mode selects the agent loop behavior.
type Mode string

const (
	ModeEdit     Mode = "edit"
	ModeGenerate Mode = "generate"
)

// Event and event kinds remain service aliases for compatibility with the
// existing Execution Flow HTTP/SSE handlers and tests.
type Event = agent.Event

const (
	EventKindRound      = agent.EventKindRound
	EventKindText       = agent.EventKindText
	EventKindReasoning  = agent.EventKindReasoning
	EventKindTool       = agent.EventKindTool
	EventKindCheckpoint = agent.EventKindCheckpoint
)

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
	FinalValid   bool    `json:"final_valid"`
	CanEnable    bool    `json:"can_enable"`
	Message      string  `json:"message,omitempty"`
	Events       []Event `json:"events"`
}

// ValidationMode identifies the purpose of a validation result.
type ValidationMode string

const (
	ValidationCheckpoint ValidationMode = "checkpoint"
	ValidationFinal      ValidationMode = "final"
)

func shouldCheckpoint(structuralRounds int, highRisk bool) bool {
	return highRisk || structuralRounds >= 3
}

func validationDelta(previous, current flow.Result) flow.Result {
	seen := map[string]struct{}{}
	key := func(e flow.ValidationError) string {
		return fmt.Sprintf("%s|%s|%s|%s", e.NodeID, e.Code, e.Level, e.Message)
	}
	for _, e := range previous.Errors {
		seen[key(e)] = struct{}{}
	}
	for _, e := range previous.Warnings {
		seen[key(e)] = struct{}{}
	}
	out := flow.Result{Errors: []flow.ValidationError{}, Warnings: []flow.ValidationError{}}
	for _, e := range current.Errors {
		if _, ok := seen[key(e)]; !ok {
			out.Errors = append(out.Errors, e)
		}
	}
	for _, e := range current.Warnings {
		if _, ok := seen[key(e)]; !ok {
			out.Warnings = append(out.Warnings, e)
		}
	}
	return out
}

// ThinkingUnset 是不设置 thinking 配置的哨兵值：选中后请求体省略 thinking
// 字段，交由 provider 自身默认行为决定是否深度思考。区别于 "disabled"
// （显式要求关闭）。
const ThinkingUnset = agent.ThinkingUnset

// thinkingMode normalizes a user-supplied reasoning mode. Empty string and
// "disabled" both turn chain-of-thought off; any other value is passed through
// (low/high/max or a provider-specific custom token).
func thinkingMode(t string) string {
	return agent.ThinkingMode(t)
}

// newAgentCompletionRequest builds a chat request for agent rounds with the
// given reasoning mode。thinking 取值可为 "disabled"/"low"/"high"/"max"
// 或任意自定义串;空串与 "disabled" 等价(关闭思维链);ThinkingUnset 时省略
// thinking 字段(交由 provider 默认)。provider 的 strict-content 设置
// (ollama/vLLM) 通过 ForceContentString 生效。
func newAgentCompletionRequest(provider openai.Provider, messages []openai.Message, thinking string) openai.CompletionRequest {
	return agent.CompletionRequest(provider, messages, thinking)
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

【规则优先级】
1. 用户明确的业务要求和决策；2. Flow 结构、执行和数据流不变量；
3. 当前模式规则；4. 工具使用流程；5. 通用测试建议；6. 参考示例。
用户明确要求优先于通用建议，但不能违反系统不变量；示例只是参考。
原始用户需求是不可替代的上下文锚点，后续校验摘要不得覆盖或改写它。

【checkpoint 与 final】
结构变更累计三轮后执行一次 checkpoint；删除节点、改变已有连线、修改已有输入来源、修改 cache-set/try/catch/loop 等高风险操作可提前触发，Agent 也可主动调用 checkpoint。checkpoint 是内部导航反馈：不暂停用户、不回滚、不冻结已检查链路，只反馈新增问题并继续生成。只有认证冲突、业务语义不清或无法安全推断的信息才询问用户。
生成结束时必须执行 final 自检：对照原始用户需求和临时目标清单，确认所有目标、数据来源、断言和异常路径；final 未通过时不得启用，也不得用“生成完毕”掩盖未完成问题。AgentFinished 只表示本轮模型停止输出，用户可以要求继续修复。

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
   - 请求体必须保持 schema 的层级：OpenAPI 整体 body 容器与 body.properties 字段只能表达一层，禁止因同时读取 params 和 request_body 而重复包裹；但 schema.properties 中合法的 body 字段不得删除
   - 对可能失败的节点用 try/catch 包裹，提供 fallback
   - 业务关键路径至少覆盖：正常场景 + 参数边界 + 异常降级
   - 数据依赖链（如"登录取 token → 用 token 调业务 API"）
     必须完整闭环，不可断链

3. 数据流显式化——每个参数都要有来处
   - api 节点的 inputs 必须根据 config.unit 里的 params/request_body
     声明每个参数的键与类型
   - OpenAPI 2 的 params 中 name=body、in=body 通常表示整个请求体容器，不是 JSON body 内的字段；当 request_body.properties 存在时，应展开 properties 作为 body 输入，禁止把容器额外包装成 body 字段。若 properties 中明确声明了名为 body 的业务字段，必须保留它，不得仅按字段名删除 body
   - config.params 必须使用真实业务字段名；字段级请求体使用 config.params.username 等形式，不要把这些字段再包进 config.params.body。只有 schema 明确存在 body 业务字段时，才允许生成 body 字段
   - 认证类参数（authorization/token/api-key）source 填 "$cache.token"
   - API input 用真实参数名作为键，source 表示取值来源，in 表示请求位置；in 只能是
     header/body/query/path，不要使用 header. 前缀伪造参数名
   - swagger 中声明为 in:header 的参数（如 X-Timestamp/X-Nonce/X-Signature
     等签名/专用头）应在 api input 中使用 in:header；自定义专用头可用 config.headers
   - 业务参数（query/path/body）的 source 引用上游输出键名
   - 任何 input 都不能没有来源——要么来自上游输出，
     要么来自 $cache.xxx，要么来自 start 入参
   - 已经完成的前置链路应尽量复用，不要为了补一个参数大面积重写或重建已有节点
 - start 节点也可以加入种子数据，但不要把所有下游 API 参数无差别批量注入 start
   - 后续 API 所需参数默认优先绑定到最近的上级 adapter 输出或 cache-set 写入的缓存；缺少来源时，就近在能提供该参数的节点补充种子数据
   - 如果参数既不是已有上游输出、缓存，也不是用户要求的种子数据，不得臆造 source；应调用 validate_flow 后向用户说明缺少数据来源
   - 如果用户明确指定某个节点补充种子数据，该节点可以是任意节点，包括 start；必须尊重用户的明确指定
 - 修改前应优先保留已有节点、连线和 source，只做满足当前需求所需的最小变更

4. 生成后自检
   - 流编排完成后调用 validate_flow 校验
   - 确认是否遗漏了关键的 CRUD 组合或状态变更序列
   - 检查 try/catch 配对是否正确，loop 的 input 是否为数组
   - AgentFinished 只表示本轮模型输出结束，不表示用户需求已完成或 Flow 可启用
   - 只要仍有未完成目标、阻断校验问题或必要修复操作，就不得主动宣布完成，应继续使用工具修复并再次校验
   - final 校验未通过时优先继续修复，不得用一句“无法完成”或“生成完毕”替代必要的工具操作
   - 只有确实缺少用户决策、业务语义或无法安全推断的数据时才暂停询问；不能把可通过已有单元、上游输出或缓存解决的问题提前交给用户
   - 最终回复前必须对照原始用户需求和临时目标清单逐项确认，未覆盖的目标必须明确标记并继续处理或说明具体缺口

节点类型:start、api(引用测试单元,unit_id 来自 list_units)、
assert、loop、try、catch、cache-set、adapter(JSONata 转换)。

【API 响应信封】api 节点执行成功时输出固定信封格式:
{"status_code": <http状态码浮点数>, "body": <响应体JSON或字符串>}
下游节点通过 body.xxx 访问响应字段,通过 status_code 访问状态码。
例如:缓存写入表达式需从 token 改为 body.token;
断言字段需从 token 改为 body.token。

【API 自定义请求头】api 节点可通过 config.headers 设置请求头(键→值),仅用于
swagger 中未声明的专有自定义头；如自定义头还需要 source，应在 inputs 中使用同名真实键，
并设置 in:header，config.headers 可只保留该键作为位置声明（值为 null 时不覆盖 source）,如 LLM 等第三方接口的专有头
(如 Content-Type、X-Api-Key、OpenAI-Organization)。对已由 swagger 声明为
in:header 的参数头,应改用 api 节点 inputs 同名键声明,执行器会自动路由为
请求头,不必(也不应)塞进 config.headers。值以 '=' 开头为
JSONata 表达式,对当前输入求值(如 {"headers":{"Authorization":"=token"}});
否则为字面量。自定义头会覆盖同名的自动认证头,其余仍按原规则路由。

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
				Description: "创建节点。type: start|api|assert|loop|try|catch|cache-set|adapter。api 需带 unit_id,inputs 使用真实参数名并可设置 source 与 in(in 只能是 header/body/query/path)，建议同时声明 config.params。api 的 config.headers 可设置自定义请求头(键→值):值以 '=' 开头为 JSONata 表达式对当前输入求值(如 \"=token\"),否则为字面量;自定义头覆盖同名的自动认证头,适用于 LLM 等第三方接口的专有头。api 响应为信封 {\"status_code\":N,\"body\":...},cache-set 需连到数据来源的上游节点,config 的 writes 值:字符串走 JSONata 求值(如 body.token)、非字符串直接当字面量、固定字符串放 static 用 $static.xxx 引用,如 {\"writes\":{\"token\":\"body.token\",\"invalid\":\"$static.bad\"},\"static\":{\"bad\":\"fake_token\"}}。adapter 若接 api 信封需用 $.body.xxx 访问字段。assert 节点 config 形状为 {\"assertions\":[{\"field\":\"status_code|body.xxx\",\"op\":\"eq|ne|contains|gt|lt\",\"expected\":期望值}]}。loop 若接 api 信封,input 指向 body.xxx。",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"parent":{"type":"string"},"inputs":{"type":"object"},"outputs":{"type":"object"},"config":{"type":"object"}},"required":["id","type"],"additionalProperties":false}`),
			},
		},
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolUpdateNode,
				Description: "更新节点的 inputs/outputs 或 config。api 节点 inputs 使用真实参数名，可编辑 source 与 in(header/body/query/path)；config.headers 可设置自定义请求头(键→值),值以 '=' 开头为 JSONata 表达式对当前输入求值,否则为字面量,自定义头覆盖同名的自动认证头。cache-set 的 config 格式:{ \"writes\": { \"<缓存key>\": <值> }, \"static\": { \"<key>\": <固定字符串> } },writes 值:字符串→JSONata 表达式求值,非字符串→字面量,字符串字面量用 static+$static.xxx。",
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
		{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        toolCheckpoint,
				Description: "主动执行一次 checkpoint 校验；不会暂停用户或回滚当前树",
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
	// 同一段对话的所有轮次复用同一个 provider 会话 ID。
	ctx = openai.WithSessionID(ctx, flowProviderSessionID(flowID, session.ID))

	history, err := unmarshalSessionMessages(session)
	if err != nil {
		return nil, err
	}

	// Rebuild the message list for this submission: system + prior history + user.
	// thinking 以数据库持久化的值为准(流级偏好),不依赖调用方传参。
	req := newAgentCompletionRequest(opt.Provider, []openai.Message{{Role: "system", Content: strPtr(flowSystemPrompt(opt.Mode, d.SystemPrompt))}}, FlowThinking(db, flowID))
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
	structuralRounds := 0
	previousValidation := flow.Result{Errors: []flow.ValidationError{}, Warnings: []flow.ValidationError{}}

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

		// 3 次重试应对瞬态失败(网络抖动/限流)，由通用 Runtime 负责。
		resp, lastErr := agent.CompleteWithRetry(ctx, opt.Provider, opt.Provider, req, func(text string) {
			if opt.Emit != nil {
				opt.Emit(Event{Kind: EventKindText, Round: round, Text: text})
			}
		}, func(text string) {
			if opt.Emit != nil {
				opt.Emit(Event{Kind: EventKindReasoning, Round: round, Text: text})
			}
		}, 3)
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
		structuralChange := false
		highRiskChange := false
		for tcIdx, tc := range msg.ToolCalls {
			log.Infof("agent: flow=%d round=%d tool=%s args=%s", flowID, round, tc.Function.Name, tc.Function.Arguments)
			result := ExecTool(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			switch tc.Function.Name {
			case toolCreateNode, toolUpdateNode, toolDeleteNode, toolLinkNodes:
				structuralChange = true
			}
			if tc.Function.Name == toolDeleteNode || tc.Function.Name == toolLinkNodes || tc.Function.Name == toolUpdateNode {
				highRiskChange = true
			}
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
				// scope 暂停前必须先落盘当前工作树。否则用户确认后
				// resumeScopePause 会从旧 draft 重建 tree，导致本轮已生成节点消失。
				if err := SnapshotAPINodes(db, tree); err != nil {
					return nil, err
				}
				if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt, d.Thinking); err != nil {
					return nil, err
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
		if structuralChange {
			structuralRounds++
			log.Infof("agent: flow=%d round=%d 结构变更 round 计数=%d", flowID, round, structuralRounds)
		}
		if structuralChange && shouldCheckpoint(structuralRounds, highRiskChange) {
			log.Infof("agent: flow=%d round=%d 开始 checkpoint 校验(structural_rounds=%d high_risk=%v)", flowID, round, structuralRounds, highRiskChange)
			current := flow.Validate(tree, flow.ValidatorOptions{})
			delta := validationDelta(previousValidation, current)
			previousValidation = current
			structuralRounds = 0
			log.Infof("agent: flow=%d round=%d checkpoint 校验完成(valid=%v new_errors=%d new_warnings=%d)", flowID, round, !current.HasErrors(), len(delta.Errors), len(delta.Warnings))
			feedback := map[string]any{
				"mode": string(ValidationCheckpoint), "valid": !current.HasErrors(),
				"errors": delta.Errors, "warnings": delta.Warnings,
			}
			req.Messages = append(req.Messages, openai.Message{Role: "system", Content: strPtr("checkpoint 校验结果:" + jsonString(feedback) + "。checkpoint 不暂停用户、不回滚当前树；优先修复新增结构错误，同时继续对照原始需求生成。")})
			ev := Event{Kind: EventKindCheckpoint, Round: round, Result: feedback}
			res.Events = append(res.Events, ev)
			if opt.Emit != nil {
				opt.Emit(ev)
			}
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
			if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt, d.Thinking); err != nil {
				return nil, err
			}
			res.Message = "已达 50 轮工具调用上限,已暂停。你可以选择继续生成或停止。"
			return res, nil
		}
	}

	// AgentFinished 与 final 校验状态分离：模型可以结束本轮，但不合格的
	// Flow 不能启用；用户可基于保留的会话继续修复。
	finalValidation := flow.Validate(tree, flow.ValidatorOptions{})
	res.FinalValid = !finalValidation.HasErrors()
	res.CanEnable = res.FinalValid
	if !res.FinalValid {
		res.Message = "Agent 已结束本轮，但 Flow 尚未通过 final 校验，可继续修复"
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
	if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), &d.SystemPrompt, d.Thinking); err != nil {
		return nil, err
	}
	return res, nil
}

// complete calls the provider, preferring streaming when available so the
// assistant's reply reaches the client token by token via onText. 若提供
// onReasoning,思考过程增量(reasoning_content)会实时回调。
func (opt AgentOptions) complete(ctx context.Context, p ChatProvider, req openai.CompletionRequest, onText func(string), onReasoning openai.ReasoningCallback) (*openai.CompletionResponse, error) {
	return agent.Complete(ctx, p, p, req, onText, onReasoning)
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
