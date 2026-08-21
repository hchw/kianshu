package jsonata

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"

	"github.com/xiatechs/jsonata-go"
)

// extsHashB64 注册"原始摘要字节 -> base64"的哈希/MAC 家族（D4）。
// 与既有 hex 版（$md5/$sha1/$sha256/$hmac）并存：hex 版把摘要渲染成
// 十六进制文本用于展示/传统签名串；b64 版输出的是原始字节的 base64，
// 用于云厂商签名等"HMAC -> base64"的刚需场景。
// 注意：不要写成 base64encode($hmac(...))——那会把 hex 文本再编码一次，
// 得到错误结果（见 2.3 的对照测试）。
func extsHashB64() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		// $md5b64(s) -> base64(md5 原始字节)
		"md5b64": {Func: func(s string) string {
			h := md5.Sum([]byte(s))
			return base64.StdEncoding.EncodeToString(h[:])
		}},
		// $sha1b64(s) -> base64(sha1 原始字节)
		"sha1b64": {Func: func(s string) string {
			h := sha1.Sum([]byte(s))
			return base64.StdEncoding.EncodeToString(h[:])
		}},
		// $sha256b64(s) -> base64(sha256 原始字节)
		"sha256b64": {Func: func(s string) string {
			h := sha256.Sum256([]byte(s))
			return base64.StdEncoding.EncodeToString(h[:])
		}},
		// $hmacb64(secret, msg, algo) -> base64(原始 HMAC 字节)
		// algo 仅支持 sha1/sha256/md5，其余报错——错误在表达式求值时
		// 携带可读信息返回，与 $hmac 行为一致。
		"hmacb64": {Func: func(secret, msg, algo string) (string, error) {
			var h func() hash.Hash
			switch algo {
			case "sha256":
				h = sha256.New
			case "sha1":
				h = sha1.New
			case "md5":
				h = md5.New
			default:
				return "", fmt.Errorf("不支持的 hmac 算法: %s（仅支持 sha1/sha256/md5）", algo)
			}
			mac := hmac.New(h, []byte(secret))
			mac.Write([]byte(msg))
			return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
		}},
	}
}
