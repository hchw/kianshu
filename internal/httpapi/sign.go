package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// signResp 是签名鉴权验证通过后的返回。
type signResp struct {
	Ok        bool   `json:"ok" example:"true"`
	Timestamp int64  `json:"timestamp" example:"1730000000"` // 请求携带的时间戳(原样回显)
	Nonce     string `json:"nonce" example:"abc123"`         // 请求携带的 nonce(原样回显)
}

// handleSign 是外部测试签名鉴权用的接口。
//
// 请求需在登录(Authorization: Bearer)之后,再携带经由签名中间件验证通过的签名头:
//
//	X-Timestamp: <Unix 秒>
//	X-Nonce:     <随机串>
//	X-Signature: <对 METHOD\npath\nquery\nbody\ntimestamp\nonce 的 HMAC-SHA256 hex>
//
// 中间件校验通过次数才进入本 handler;由此可用于确认客户端端的签名计算与服务端一致。
//
//	@Summary	测试签名鉴权
//	@Description	登录后携带 X-Timestamp / X-Nonce / X-Signature 请求头;签名中间件用共享密钥(KS_SIGN_SECRET)对 METHOD、path、query、body、timestamp、nonce 组成的规范串计算 HMAC-SHA256 并校验,通过后返回成功。用于外部联调签名算法。
//	@Tags		工具
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		X-Timestamp	header		int64	true	"Unix 秒级时间戳"
//	@Param		X-Nonce		header		string	true	"随机串,防重放"
//	@Param		X-Signature	header		string	true	"hex HMAC-SHA256 签名"
//	@Success	200			{object}	signResp	"签名校验通过"
//	@Failure	401			{object}	errorResp	"签名缺失 / 时间戳超差 / nonce 重放 / 校验失败"
//	@Router		/utils/sign [post]
func (s *Server) handleSign(c *gin.Context) {
	ts, _ := strconv.ParseInt(c.GetHeader(sigHeaderTimestamp), 10, 64)
	writeJSON(c, http.StatusOK, signResp{
		Ok:        true,
		Timestamp: ts,
		Nonce:     c.GetHeader(sigHeaderNonce),
	})
}
