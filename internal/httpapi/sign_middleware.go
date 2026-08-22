package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// 签名鉴权使用的请求头。
const (
	sigHeaderTimestamp = "X-Timestamp" // Unix 秒级时间戳
	sigHeaderNonce     = "X-Nonce"     // 随机串,防重放
	sigHeaderSignature = "X-Signature" // hex HMAC-SHA256 签名
)

// sigMaxSkew 允许的请求时间戳与服务器时间最大偏差。
const sigMaxSkew = 5 * time.Minute

// signCanonical 组装待签名的规范串:
//
//	METHOD\npath\nquery\nbody\ntimestamp\nnonce
//
// 会读取并回填请求体,保证后续 handler 仍能读到 body。
func (s *Server) signCanonical(c *gin.Context) (string, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return strings.Join([]string{
		c.Request.Method,
		c.Request.URL.Path,
		c.Request.URL.RawQuery,
		string(body),
		c.GetHeader(sigHeaderTimestamp),
		c.GetHeader(sigHeaderNonce),
	}, "\n"), nil
}

// signMiddleware 校验请求携带的 HMAC-SHA256 签名(签名鉴权)。
//
// 调用方(如外部测试签名接口)需与服务端共享同一密钥(KS_SIGN_SECRET),
// 并对规范串 METHOD\npath\nquery\nbody\ntimestamp\nnonce 计算签名后放入请求头:
//
//	X-Timestamp: <Unix 秒>
//	X-Nonce:     <随机串>
//	X-Signature: <hex HMAC-SHA256>
//
// 校验点:字段齐全、时间戳新鲜(±5 分钟)、nonce 未重放、签名一致。任一项不满足
// 均返回 401 并中断请求。
func (s *Server) signAuth() gin.HandlerFunc {
	seen := newNonceSeen(sigMaxSkew)
	return func(c *gin.Context) {
		tsStr := c.GetHeader(sigHeaderTimestamp)
		nonce := c.GetHeader(sigHeaderNonce)
		sig := c.GetHeader(sigHeaderSignature)
		if tsStr == "" || nonce == "" || sig == "" {
			writeErr(c, http.StatusUnauthorized, "缺少签名头 X-Timestamp / X-Nonce / X-Signature")
			c.Abort()
			return
		}
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			writeErr(c, http.StatusUnauthorized, "签名时间戳不合法")
			c.Abort()
			return
		}
		skew := now().Unix() - ts
		if skew > int64(sigMaxSkew.Seconds()) || skew < -int64(sigMaxSkew.Seconds()) {
			writeErr(c, http.StatusUnauthorized, "签名时间戳超出允许偏差范围")
			c.Abort()
			return
		}
		if !seen.attest(nonce) {
			writeErr(c, http.StatusUnauthorized, "nonce 已使用(疑似重放)")
			c.Abort()
			return
		}
		canonical, err := s.signCanonical(c)
		if err != nil {
			writeErr(c, http.StatusUnauthorized, "读取请求体失败")
			c.Abort()
			return
		}
		expect := computeSignature(s.Cfg.SignSecret, canonical)
		got, err := hex.DecodeString(sig)
		if err != nil {
			writeErr(c, http.StatusUnauthorized, "签名格式不合法(须为 hex)")
			c.Abort()
			return
		}
		if subtle.ConstantTimeCompare(expect, got) != 1 {
			log.WithFields(log.Fields{
				"method":       c.Request.Method,
				"path":         c.Request.URL.Path,
				"query":        c.Request.URL.RawQuery,
				"ts_header":    tsStr,
				"nonce_header": nonce,
				"canonical":    canonical,
				"expect_hex":   hex.EncodeToString(expect),
				"got_hex":      sig,
			}).Errorf("签名校验失败: 实际参与签名的规范串及期望/收到签名")
			writeErr(c, http.StatusUnauthorized, "签名校验失败")
			c.Abort()
			return
		}
		c.Next()
	}
}

// computeSignature 用密钥对规范串计算 HMAC-SHA256,返回其原字节串。
func computeSignature(secret, canonical string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return mac.Sum(nil)
}

// nonceSeen 记录在时间窗口内见过的 nonce,用于重放防护。
type nonceSeen struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func newNonceSeen(ttl time.Duration) *nonceSeen {
	return &nonceSeen{seen: make(map[string]time.Time), ttl: ttl}
}

// attest 记录 nonce;若其已存在(重放)或早于 TTL 窗口则返回 false。
func (n *nonceSeen) attest(nonce string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for k, t := range n.seen {
		if now.Sub(t) > n.ttl {
			delete(n.seen, k)
		}
	}
	key := nonce
	if _, ok := n.seen[key]; ok {
		return false
	}
	n.seen[key] = now
	return true
}
