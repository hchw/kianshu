package jsonata

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xiatechs/jsonata-go"
)

// extsTime 注册时间戳与唯一标识扩展（D1：每函数一个注册；D2：输入输出统一 string）。
// JSONata 表达式中的返回值均为字符串，便于与签名串拼接。
func extsTime() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		// $now() -> Unix 秒（十进制字符串）
		"now": {Func: func() string {
			return fmt.Sprintf("%d", time.Now().Unix())
		}},
		// $nowMs() -> Unix 毫秒
		"nowMs": {Func: func() string {
			return fmt.Sprintf("%d", time.Now().UnixMilli())
		}},
		// $nowISO() -> 当前 UTC 时间的 RFC3339 字符串，如 2026-08-20T15:04:05Z
		"nowISO": {Func: func() string {
			return time.Now().UTC().Format(time.RFC3339)
		}},
		// $uuid() -> UUID v4 字符串，取自 crypto/rand 的 google/uuid 实现
		"uuid": {Func: func() string {
			return uuid.NewString()
		}},
	}
}
