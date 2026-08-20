package jsonata

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/xiatechs/jsonata-go"
)

// extsEncoding 注册编码类扩展（hex / URL 百分号编码）。
// D2：与既有 base64encode/base64decode 一样，输入输出统一为 string。

// urlEncodeBytes 按 RFC 3986 对整串做百分号编码：除 unreserved
// （A-Z a-z 0-9 - . _ ~）外的每个字节编码为 %XX。与 url.QueryEscape
// （空格转 '+'，用于表单）不同，适合签名串构造等确定性场景。
func urlEncodeBytes(s string) string {
	const upperHex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0f])
		}
	}
	return b.String()
}

func urlDecodeBytes(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("url 解码失败: %q 处存在不完整的 %% 转义", s[i:])
		}
		hi := uint8(hexNibble(s[i+1]))
		lo := uint8(hexNibble(s[i+2]))
		if hi == 0xff || lo == 0xff {
			return "", fmt.Errorf("url 解码失败: %q 不是合法的十六进制转义", s[i:i+3])
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0xff
}

func extsEncoding() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		// $hexencode(s) -> 字符串 UTF-8 字节的小写十六进制
		"hexencode": {Func: func(s string) string {
			return hex.EncodeToString([]byte(s))
		}},
		// $hexdecode(hex) -> 十六进制解码回字符串；非法长度/字符时返回错误
		"hexdecode": {Func: func(s string) (string, error) {
			if len(s)%2 != 0 {
				return "", fmt.Errorf("hex 解码失败: 长度 %d 为奇数", len(s))
			}
			b, err := hex.DecodeString(s)
			if err != nil {
				return "", fmt.Errorf("hex 解码失败: %v", err)
			}
			return string(b), nil
		}},
		// $urlencode(s) -> RFC 3986 百分号编码
		"urlencode": {Func: func(s string) string {
			return urlEncodeBytes(s)
		}},
		// $urldecode(s) -> 百分号解码；非法转义时返回错误
		"urldecode": {Func: func(s string) (string, error) {
			return urlDecodeBytes(s)
		}},
	}
}
