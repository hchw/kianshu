package vendor

import "strings"

// opencode 要求编程 Agent 流量带专属 User-Agent 标识自身,并在每段对话中
// 通过 x-opencode-session 请求头发送稳定会话 ID,便于其优化路由与提示词缓存。
func init() {
	register(Vendor{
		Name:          "opencode",
		UserAgent:     CodingAgentUserAgent,
		SessionHeader: "x-opencode-session",
		match: func(baseURL string) bool {
			return strings.Contains(strings.ToLower(baseURL), "opencode")
		},
	})
}
