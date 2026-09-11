// Package vendor 承载各 OpenAI 兼容厂商在 HTTP 请求层面的适配差异。
//
// 每个厂商一个文件(vendor_<name>.go),在 init 时向本包注册;
// 新增厂商只需新增文件,无需改动本文件或 openai 客户端逻辑。
package vendor

import "net/http"

// CodingAgentUserAgent 是本客户端作为编程 Agent 的专属 User-Agent 标识,
// 而不是通用 SDK / HTTP 库名称。
const CodingAgentUserAgent = "kianshu-coding-agent/1.0"

// Vendor 描述一个厂商的请求适配规则。
type Vendor struct {
	// Name 便于日志与测试识别。
	Name string
	// UserAgent 非空时覆盖请求的 User-Agent。
	UserAgent string
	// SessionHeader 非空时,把当前对话的会话 ID 写入该请求头,
	// 便于厂商侧优化路由与提示词缓存。
	SessionHeader string
	// match 判断 provider 地址是否属于该厂商(按 URL 识别)。
	match func(baseURL string) bool
}

// registered 是已注册的厂商适配器,按注册顺序匹配,首个命中者生效。
var registered []Vendor

// register 由各厂商文件的 init 调用,登记一个适配器。
func register(v Vendor) {
	registered = append(registered, v)
}

// Match 返回匹配 provider 地址的厂商适配器;无匹配时返回 nil。
func Match(baseURL string) *Vendor {
	for i := range registered {
		if registered[i].match != nil && registered[i].match(baseURL) {
			return &registered[i]
		}
	}
	return nil
}

// ApplyHeaders 依据 provider 地址写入厂商特有的请求头。
// sessionID 为空时不写会话头(仍会写 User-Agent)。
func ApplyHeaders(h http.Header, baseURL, sessionID string) {
	v := Match(baseURL)
	if v == nil {
		return
	}
	if v.UserAgent != "" {
		h.Set("User-Agent", v.UserAgent)
	}
	if v.SessionHeader != "" && sessionID != "" {
		h.Set(v.SessionHeader, sessionID)
	}
}
