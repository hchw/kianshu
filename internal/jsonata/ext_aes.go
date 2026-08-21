package jsonata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/xiatechs/jsonata-go"
)

// --------------------------------------------
// AES 对称加解密扩展（D3）。
//
// 参数语义（输入输出统一 string，D2）：
//   - plain / cipher：明文为普通字符串（UTF-8 字节即被加密内容）；
//     密文一律以标准 base64 表示（含 $aesEncrypt 的输出与 $aesDecrypt 的输入）。
//   - key / iv：字符串，支持三种派生方式（前缀区分，常量策略）：
//       base64:<b64>  二进制 key/iv 的 base64 表示（严格校验）
//       hex:<hex>     二进制 key/iv 的十六进制表示（严格校验）
//       裸 UTF-8      口令式字符串（按归一策略调整长度）
//   - mode：cbc | ecb | ctr，其余报可读错误。
//
// 归一策略（常量表固化，见 normalizeKeyLen / normalizeIVLen，单测锁定）：
//   - key 目标长度仅接受 16 / 24 / 32 字节（AES-128/192/256）：
//       base64:/hex: 解码后必须恰好命中三者之一，否则报可读错误
//       （二进制 key 是精确字节语义，不猜测、不静默改写）；
//       裸 UTF-8 口令按 不足补零 / 超长截断 归一到最近的合法长度
//       （<=16 补 0 到 16；<=24 补 0 到 24；更长一律截断为 32）。
//   - iv 一律归一为 16 字节（AES 分组大小）：不足右补 0x00，超长取前 16。
//
// 填充统一 PKCS7；解密侧严格校验填充合法性，非法填充返回可读错误而非静默结果。
// --------------------------------------------

// aesKeyLens 允许的 AES key 字节长度（128/192/256 位）。
var aesKeyLens = map[int]bool{16: true, 24: true, 32: true}

// normalizeKeyLen 返回裸 UTF-8 key 应归一到的合法长度。
func normalizeKeyLen(n int) int {
	switch {
	case n <= 16:
		return 16
	case n <= 24:
		return 24
	default:
		return 32
	}
}

// deriveKeyBytes 把 key 字符串按派生前缀解析为原始字节，并按上述策略归一。
func deriveKeyBytes(key string) ([]byte, error) {
	raw, prefixed, err := parsePrefixedBytes(key)
	if err != nil {
		return nil, err
	}
	if prefixed {
		// 二进制 key：精确长度语义，必须命中 16/24/32，否则报可读错误。
		if !aesKeyLens[len(raw)] {
			return nil, fmt.Errorf("aes key 长度非法: %d 字节（base64:/hex: 形式的 key 仅接受 16/24/32 字节，对应 AES-128/192/256）", len(raw))
		}
		return raw, nil
	}
	// 裸 UTF-8 口令：不足补零 / 超长截断 归一到最近的合法长度。
	target := normalizeKeyLen(len(raw))
	if target < len(raw) {
		raw = raw[:target]
	} else if target > len(raw) {
		raw = append(raw, make([]byte, target-len(raw))...)
	}
	return raw, nil
}

// deriveIVBytes 把 iv 字符串解析为原始字节并归一为 16 字节。
func deriveIVBytes(iv string) ([]byte, error) {
	raw, _, err := parsePrefixedBytes(iv)
	if err != nil {
		return nil, err
	}
	return normalizeIV(raw), nil
}

func normalizeIV(raw []byte) []byte {
	const block = 16
	if len(raw) >= block {
		return raw[:block]
	}
	out := make([]byte, block)
	copy(out, raw)
	return out
}

// parsePrefixedBytes 按 base64:/hex: 前缀解析；无前缀返回裸 UTF-8 字节。
// 第二返回值标记是否为前缀（严格）形式。
func parsePrefixedBytes(s string) ([]byte, bool, error) {
	switch {
	case strings.HasPrefix(s, "base64:"):
		b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "base64:"))
		if err != nil {
			return nil, true, fmt.Errorf("base64: key/iv 解码失败: %v", err)
		}
		return b, true, nil
	case strings.HasPrefix(s, "hex:"):
		b, err := hex.DecodeString(strings.TrimPrefix(s, "hex:"))
		if err != nil {
			return nil, true, fmt.Errorf("hex: key/iv 解码失败: %v", err)
		}
		return b, true, nil
	default:
		return []byte(s), false, nil
	}
}

// pkcs7Pad 按块大小补齐（至少补 1 字节，便于去填充校验）。
func pkcs7Pad(data []byte, blockSize int) []byte {
	n := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...)
}

// pkcs7Unpad 去除 PKCS7 填充并校验合法性；非法填充返回可读错误。
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("pkcs7 填充校验失败: 数据长度 %d 不是块大小 %d 的整数倍", len(data), blockSize)
	}
	n := int(data[len(data)-1])
	if n < 1 || n > blockSize || n > len(data) {
		return nil, fmt.Errorf("pkcs7 填充校验失败: 末字节 %d 不在合法填充范围", n)
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, fmt.Errorf("pkcs7 填充校验失败: 填充字节不一致")
		}
	}
	return data[:len(data)-n], nil
}

// ecbEncryptBlocks / ecbDecryptBlocks：Go 标准库无原生 ECB，
// 用 cipher.Block 接口对整个数据按 16 字节分组逐块变换拼接（D3）。
func ecbEncryptBlocks(block cipher.Block, dst, src []byte) {
	size := block.BlockSize()
	for i := 0; i < len(src); i += size {
		block.Encrypt(dst[i:i+size], src[i:i+size])
	}
}

func ecbDecryptBlocks(block cipher.Block, dst, src []byte) {
	size := block.BlockSize()
	for i := 0; i < len(src); i += size {
		block.Decrypt(dst[i:i+size], src[i:i+size])
	}
}

// aesCrypt 执行单次对称变换；encrypt=true 为加密。
func aesCrypt(block cipher.Block, mode string, iv []byte, data []byte, encrypt bool) ([]byte, error) {
	switch mode {
	case "cbc":
		if encrypt {
			out := make([]byte, len(data))
			cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
			return out, nil
		}
		out := make([]byte, len(data))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
		return out, nil
	case "ctr":
		out := make([]byte, len(data))
		cipher.NewCTR(block, iv).XORKeyStream(out, data)
		return out, nil
	case "ecb":
		out := make([]byte, len(data))
		if encrypt {
			ecbEncryptBlocks(block, out, data)
		} else {
			ecbDecryptBlocks(block, out, data)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("不支持的 aes 模式: %s（仅支持 cbc/ecb/ctr）", mode)
	}
}

func aesSetup(key, iv, mode string) (cipher.Block, []byte, error) {
	kb, err := deriveKeyBytes(key)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(kb)
	if err != nil {
		return nil, nil, fmt.Errorf("aes key 初始化失败（应为 16/24/32 字节）: %v", err)
	}
	ivb, err := deriveIVBytes(iv)
	if err != nil {
		return nil, nil, err
	}
	return block, ivb, nil
}

// isBlockMode 判断是否走分组填充语义（cbc/ecb），ctr 为流模式不填充。
func isBlockMode(mode string) bool {
	return mode == "cbc" || mode == "ecb"
}

// extsAES 注册 $aesEncrypt / $aesDecrypt。
func extsAES() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		// $aesEncrypt(plain, key, iv, mode) -> base64 密文
		"aesEncrypt": {Func: func(plain, key, iv, mode string) (string, error) {
			block, ivb, err := aesSetup(key, iv, mode)
			if err != nil {
				return "", err
			}
			data := []byte(plain)
			if isBlockMode(mode) {
				// 分组模式：PKCS7 填充（至少补 1 字节）
				data = pkcs7Pad(data, block.BlockSize())
			}
			out, err := aesCrypt(block, mode, ivb, data, true)
			if err != nil {
				return "", err
			}
			return base64.StdEncoding.EncodeToString(out), nil
		}},
		// $aesDecrypt(cipherB64, key, iv, mode) -> 明文
		"aesDecrypt": {Func: func(cipherB64, key, iv, mode string) (string, error) {
			block, ivb, err := aesSetup(key, iv, mode)
			if err != nil {
				return "", err
			}
			ciphertext, err := base64.StdEncoding.DecodeString(cipherB64)
			if err != nil {
				return "", fmt.Errorf("aes 密文 base64 解码失败: %v", err)
			}
			if isBlockMode(mode) && (len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0) {
				return "", fmt.Errorf("aes 密文长度 %d 不是块大小 %d 的整数倍", len(ciphertext), block.BlockSize())
			}
			out, err := aesCrypt(block, mode, ivb, ciphertext, false)
			if err != nil {
				return "", err
			}
			// CTR 为流模式无填充语义：直接还原。CBC/ECB 严格校验 PKCS7 填充。
			if mode == "ctr" {
				return string(out), nil
			}
			plain, err := pkcs7Unpad(out, block.BlockSize())
			if err != nil {
				return "", err
			}
			return string(plain), nil
		}},
	}
}
