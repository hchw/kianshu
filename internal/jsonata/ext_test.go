package jsonata

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mustEval 求值或失败。
func mustEval(t *testing.T, expr string, data any) any {
	t.Helper()
	v, err := Eval(expr, data)
	if err != nil {
		t.Fatalf("Eval(%q) 意外错误: %v", expr, err)
	}
	return v
}

func mustEvalErr(t *testing.T, expr string, data any) error {
	t.Helper()
	_, err := Eval(expr, data)
	if err == nil {
		t.Fatalf("Eval(%q) 应报错，却成功了", expr)
	}
	return err
}

// 1.1 时间戳与 UUID 扩展
func TestExtTime(t *testing.T) {
	t.Run("now 秒级", func(t *testing.T) {
		before := time.Now().Unix()
		v, ok := mustEval(t, "$now()", nil).(string)
		if !ok || len(v) != 10 {
			t.Fatalf("$now() 应为 10 位十进制字符串, got %v", mustEval(t, "$now()", nil))
		}
		var got int64
		for _, c := range v {
			if c < '0' || c > '9' {
				t.Fatalf("$now() 含非数字: %q", v)
			}
		}
		got = parseDecimal(t, v)
		if got < before || got > before+5 {
			t.Fatalf("$now() 偏离当前时间: %d vs %d", got, before)
		}
	})
	t.Run("nowMs", func(t *testing.T) {
		v := mustEval(t, "$nowMs()", nil).(string)
		if len(v) != 13 {
			t.Fatalf("$nowMs() 应为 13 位毫秒时间戳, got %q", v)
		}
	})

	t.Run("nowISO RFC3339", func(t *testing.T) {
		v := mustEval(t, "$nowISO()", nil).(string)
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t.Fatalf("$nowISO() 不是合法 RFC3339: %q (%v)", v, err)
		}
		if time.Since(ts) > 5*time.Second || time.Since(ts) < -5*time.Second {
			t.Fatalf("$nowISO() 偏离当前时间: %v", v)
		}
	})

	t.Run("uuid v4 形态", func(t *testing.T) {
		v := mustEval(t, "$uuid()", nil).(string)
		parts := strings.Split(v, "-")
		if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
			t.Fatalf("$uuid() 形态非法: %q", v)
		}
		if parts[2][0] != '4' {
			t.Fatalf("$uuid() 应为 v4（第 3 段以 4 开头）: %q", v)
		}
		v2 := mustEval(t, "$uuid()", nil).(string)
		if v == v2 {
			t.Fatalf("$uuid() 连续两次相同: %q", v)
		}
	})
}

func parseDecimal(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

// 1.2 编码扩展
func TestExtEncoding(t *testing.T) {
	t.Run("hex 往返", func(t *testing.T) {
		if v := mustEval(t, `$hexencode('hello')`, nil); v != "68656c6c6f" {
			t.Fatalf("hexencode: %v", v)
		}
		if v := mustEval(t, `$hexdecode('68656c6c6f')`, nil); v != "hello" {
			t.Fatalf("hexdecode: %v", v)
		}
		// 中文 UTF-8 字节
		if v := mustEval(t, `$hexencode('中')`, nil); v != "e4b8ad" {
			t.Fatalf("hexencode 中文: %v", v)
		}
	})
	t.Run("hexdecode 非法输入", func(t *testing.T) {
		// 奇数长度
		if err := mustEvalErr(t, `$hexdecode('abc')`, nil); !strings.Contains(err.Error(), "奇数") {
			t.Fatalf("奇数长度错误信息: %v", err)
		}
		// 非十六进制字符
		if err := mustEvalErr(t, `$hexdecode('zz')`, nil); !strings.Contains(err.Error(), "hex 解码失败") {
			t.Fatalf("非法字符错误信息: %v", err)
		}
	})

	t.Run("urlencode RFC3986", func(t *testing.T) {
		// 空格 -> %20（区别于表单转义的 +）；保留字被转义
		if v := mustEval(t, `$urlencode('a b&c=d/中')`, nil); v != "a%20b%26c%3Dd%2F%E4%B8%AD" {
			t.Fatalf("urlencode: %v", v)
		}
		// unreserved 保留
		if v := mustEval(t, `$urlencode('AZaz09-._~')`, nil); v != "AZaz09-._~" {
			t.Fatalf("urlencode unreserved: %v", v)
		}
	})
	t.Run("urldecode 往返与非法输入", func(t *testing.T) {
		if v := mustEval(t, `$urldecode('a%20b%26c')`, nil); v != "a b&c" {
			t.Fatalf("urldecode: %v", v)
		}
		// 小写 %xx 也接受
		if v := mustEval(t, `$urldecode('%e4%b8%ad')`, nil); v != "中" {
			t.Fatalf("urldecode 中文: %v", v)
		}
		if err := mustEvalErr(t, `$urldecode('a%')`, nil); !strings.Contains(err.Error(), "不完整") {
			t.Fatalf("截断转义错误信息: %v", err)
		}
		if err := mustEvalErr(t, `$urldecode('%zz')`, nil); !strings.Contains(err.Error(), "十六进制") {
			t.Fatalf("非法转义错误信息: %v", err)
		}
	})
}

// 2.1 哈希 b64 已知向量
func TestExtHashB64KnownVectors(t *testing.T) {
	cases := []struct {
		expr, want string
	}{
		{`$sha256b64('hello')`, "LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="},                                                    // sha256("hello")
		{`$sha1b64('hello')`, "qvTGHdzF6KLavt4PO0gs2a6pQ00="},                                                                      // sha1("hello")
		{`$md5b64('hello')`, "XUFAKrxLKna5cZ2REBfFkg=="},                                                                           // md5("hello")
		{`$hmacb64('key','The quick brown fox jumps over the lazy dog','sha256')`, "97yD9DBThCSxMpjmqm+xQ+9NWaFJRhdZl0edvC0aPNg="}, // hmac-sha256 已知向量
	}
	for _, c := range cases {
		if v := mustEval(t, c.expr, nil); v != c.want {
			t.Errorf("%s 结果 %q, 期望 %q", c.expr, v, c.want)
		}
	}
}

// 2.1/2.2 错误分支
func TestExtHashB64Errors(t *testing.T) {
	if err := mustEvalErr(t, `$hmacb64('k','m','sha512')`, nil); !strings.Contains(err.Error(), "不支持的 hmac 算法") {
		t.Fatalf("非法算法错误信息: %v", err)
	}
}

// 2.3 hex 版与 b64 版输出不同、用途不同：b64 版是原始字节 base64，
// 绝不能写成 base64encode($hmac(...))（那会把 hex 文本再 base64 一次）。
func TestHashB64VsHexBase64Difference(t *testing.T) {
	// base64encode($sha256('hello')) —— 错误写法，输出 hex 文本的 base64
	wrong := mustEval(t, `$base64encode($sha256('hello'))`, nil)
	right := mustEval(t, `$sha256b64('hello')`, nil)
	if wrong == right {
		t.Fatalf("b64 版与 base64encode(hex版) 不应相等: %v", wrong)
	}
	if right != "LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ=" {
		t.Fatalf("b64 版结果错误: %v", right)
	}
	t.Run("hex 版仍可用作十六进制摘要", func(t *testing.T) {
		if v := mustEval(t, `$hmac('key','msg','sha256')`, nil); len(v.(string)) != 64 {
			t.Fatalf("hmac hex 版长度: %v", v)
		}
	})
}

// 3.1 key/iv 派生与长度归一策略
func TestAESKeyDerivationPolicy(t *testing.T) {
	t.Run("裸 UTF-8 口令归一", func(t *testing.T) {
		// 15 字节口令 -> 补零到 16（等价于 16 字节二进制 key）
		k1, err := deriveKeyBytes("123456789012345")
		if err != nil || len(k1) != 16 || k1[15] != 0 {
			t.Fatalf("15 字节口令应补 1 个零到 16: len=%d err=%v", len(k1), err)
		}
		// 20 字节 -> 补零到 24
		k2, err := deriveKeyBytes("abcdefghij0123456789")
		if err != nil || len(k2) != 24 {
			t.Fatalf("20 字节口令应补零到 24: len=%d err=%v", len(k2), err)
		}
		// 40 字节 -> 截断到 32
		k3, err := deriveKeyBytes(strings.Repeat("x", 40))
		if err != nil || len(k3) != 32 {
			t.Fatalf("40 字节口令应截断到 32: len=%d err=%v", len(k3), err)
		}
	})
	t.Run("base64:/hex: 前缀 key 严格校验", func(t *testing.T) {
		kb, err := deriveKeyBytes("base64:" + base64.StdEncoding.EncodeToString([]byte("1234567890123456")))
		if err != nil || len(kb) != 16 {
			t.Fatalf("base64: 16 字节 key 应通过: len=%d err=%v", len(kb), err)
		}
		if _, err := deriveKeyBytes("base64:" + base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
			t.Fatal("base64: 非 16/24/32 字节的 key 应报错")
		} else if !strings.Contains(err.Error(), "仅接受 16/24/32") {
			t.Fatalf("长度错误信息: %v", err)
		}
		kh, err := deriveKeyBytes("hex:" + strings.Repeat("00", 24))
		if err != nil || len(kh) != 24 {
			t.Fatalf("hex: 24 字节 key 应通过: len=%d err=%v", len(kh), err)
		}
		if _, err := deriveKeyBytes("hex:zz"); err == nil {
			t.Fatal("hex: 非法字符应报错")
		}
		if _, err := deriveKeyBytes("base64:!!!"); err == nil {
			t.Fatal("base64: 非法 base64 应报错")
		}
	})
	t.Run("iv 归一到 16 字节", func(t *testing.T) {
		if iv, _ := deriveIVBytes("iv"); len(iv) != 16 || iv[0] != 'i' || iv[15] != 0 {
			t.Fatalf("短 iv 应右补零到 16: %x", iv)
		}
		if iv, _ := deriveIVBytes(strings.Repeat("v", 20)); len(iv) != 16 {
			t.Fatalf("长 iv 应截断到 16: len=%d", len(iv))
		}
		iv, _ := deriveIVBytes("hex:" + strings.Repeat("ab", 16))
		if len(iv) != 16 || iv[0] != 0xab {
			t.Fatalf("hex: iv 解析: %x", iv)
		}
	})
}

// 3.2 PKCS7 填充/去填充
func TestPKCS7(t *testing.T) {
	if p := pkcs7Pad([]byte("hello"), 16); len(p) != 16 || p[15] != 11 {
		t.Fatalf("pkcs7 填充: %x", p)
	}
	u, err := pkcs7Unpad(pkcs7Pad([]byte("hello"), 16), 16)
	if err != nil || string(u) != "hello" {
		t.Fatalf("pkcs7 去填充: %q err=%v", u, err)
	}
	// 空数据（整块填充）
	u, err = pkcs7Unpad(pkcs7Pad(nil, 16), 16)
	if err != nil || len(u) != 0 {
		t.Fatalf("整块填充去填充: %q err=%v", u, err)
	}
	// 非法填充：长度非块倍数 / 填充字节不一致 / 末字节越界
	for _, bad := range [][]byte{{1, 2, 3}, {0: 16, 1: 15}, []byte("123456789012345X")} {
		if _, err := pkcs7Unpad(bad, 16); err == nil {
			t.Fatalf("非法填充应报错: %x", bad)
		}
	}
}

// 3.3/3.4/3.5 三种模式的加解密往返
func TestAESRoundTrip(t *testing.T) {
	plain := "hello 鉴枢"
	key := "0123456789abcdef" // 16 字节口令，iv 裸串，走归一
	iv := "0123456789abcdef"  // 16 字节
	for _, mode := range []string{"cbc", "ctr", "ecb"} {
		t.Run(mode, func(t *testing.T) {
			ct, err := Eval(`$aesEncrypt('`+plain+`','`+key+`','`+iv+`','`+mode+`')`, nil)
			if err != nil {
				t.Fatalf("加密失败: %v", err)
			}
			// 密文是合法 base64
			if _, err := base64.StdEncoding.DecodeString(ct.(string)); err != nil {
				t.Fatalf("密文非 base64: %v", err)
			}
			pt, err := Eval(`$aesDecrypt('`+ct.(string)+`','`+key+`','`+iv+`','`+mode+`')`, nil)
			if err != nil {
				t.Fatalf("解密失败: %v", err)
			}
			if pt != plain {
				t.Fatalf("往返不一致: %q != %q", pt, plain)
			}
		})
	}
}

// 3.6 错误分支：非法 mode / key 长度 / 填充校验失败 / 密文损坏
func TestAESErrorBranches(t *testing.T) {
	ct, _ := Eval(`$aesEncrypt('hello','0123456789abcdef','0123456789abcdef','cbc')`, nil)
	t.Run("非法 mode", func(t *testing.T) {
		_, err := Eval(`$aesEncrypt('hello','0123456789abcdef','0123456789abcdef','gcm')`, nil)
		if err == nil || !strings.Contains(err.Error(), "仅支持 cbc/ecb/ctr") {
			t.Fatalf("非法 mode 应报可读错误: %v", err)
		}
	})
	t.Run("DES/3DES 不可用", func(t *testing.T) {
		// 7.2 要求：DES/3DES 明确不在支持集内（行业基线为 AES）
		for _, m := range []string{"des", "des-ede3-cbc", "3des", "DES", "3DES-CBC"} {
			_, err := Eval(`$aesEncrypt('hello','0123456789abcdef','0123456789abcdef','`+m+`')`, nil)
			if err == nil || !strings.Contains(err.Error(), "仅支持 cbc/ecb/ctr") {
				t.Fatalf("mode %q 应报不可用: %v", m, err)
			}
		}
	})
	t.Run("base64: key 长度非法", func(t *testing.T) {
		_, err := Eval(`$aesEncrypt('hello','base64:c2hvcnQ=','0123456789abcdef','cbc')`, nil)
		if err == nil || !strings.Contains(err.Error(), "仅接受 16/24/32") {
			t.Fatalf("key 长度错误信息: %v", err)
		}
	})
	t.Run("解密填充校验失败", func(t *testing.T) {
		// 篡改密文末字节 -> PKCS7 校验应失败并给出可读错误
		b, _ := base64.StdEncoding.DecodeString(ct.(string))
		b[len(b)-1] ^= 0xff
		tampered := base64.StdEncoding.EncodeToString(b)
		_, err := Eval(`$aesDecrypt('`+tampered+`','0123456789abcdef','0123456789abcdef','cbc')`, nil)
		if err == nil || !strings.Contains(err.Error(), "pkcs7") {
			t.Fatalf("篡改密文应报填充错误: %v", err)
		}
	})
	t.Run("密文非 base64", func(t *testing.T) {
		_, err := Eval(`$aesDecrypt('not-base64!!','0123456789abcdef','0123456789abcdef','cbc')`, nil)
		if err == nil || !strings.Contains(err.Error(), "base64 解码失败") {
			t.Fatalf("坏密文应报错误: %v", err)
		}
	})
	t.Run("CTR 往返不受填充影响", func(t *testing.T) {
		// CTR 对任意长度明文直接流式变换：明文长度非块倍数也合法
		ctr, err := Eval(`$aesEncrypt('abc','0123456789abcdef','0123456789abcdef','ctr')`, nil)
		if err != nil {
			t.Fatalf("ctr 加密失败: %v", err)
		}
		pt, err := Eval(`$aesDecrypt('`+ctr.(string)+`','0123456789abcdef','0123456789abcdef','ctr')`, nil)
		if err != nil || pt != "abc" {
			t.Fatalf("ctr 非块长度明文往返: %q err=%v", pt, err)
		}
	})
}

// 4.1/4.2 RSA 加解密与签名往返（PKCS#1 + PKCS#8 双 PEM 格式）
func TestRSARoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}
	privPKCS1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	pubPKCS1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&priv.PublicKey)})
	privPKCS8B, _ := x509.MarshalPKCS8PrivateKey(priv)
	privPKCS8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privPKCS8B})
	pubPKCS8B, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPKCS8 := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubPKCS8B})

	// 注入求值环境，避免在表达式里内嵌大段 PEM（JSONata 字符串字面量无多行转义）
	msg := `{"order":"abc","amt":100}`
	t.Run("OAEP 加解密 PKCS#1", func(t *testing.T) {
		for i, pub := range []string{string(pubPKCS1), string(pubPKCS8)} {
			ct, err := Eval(`$rsaEncrypt('`+msg+`',$pubX)`+"", map[string]any{})
			_ = ct
			// 直接用 Go 函数路径测（表达式内嵌 PEM 不可行，见 helper）
			pt, err := rsaDecryptOAEPHelper(t, priv, pub, msg)
			if err != nil {
				t.Fatalf("PKCS#1 往返失败[%d]: %v", i, err)
			}
			if pt != msg {
				t.Fatalf("往返不一致[%d]: %q", i, pt)
			}
		}
	})
	t.Run("签名/验签", func(t *testing.T) {
		for i, privPEM := range []string{string(privPKCS1), string(privPKCS8)} {
			for _, algo := range []string{"sha1", "sha256"} {
				sig, err := rsaSignHelper(t, msg, privPEM, algo)
				if err != nil {
					t.Fatalf("签名失败[%d %s]: %v", i, algo, err)
				}
				for j, pubPEM := range []string{string(pubPKCS1), string(pubPKCS8)} {
					ok, err := rsaVerifyHelper(t, msg, sig, pubPEM, algo)
					if err != nil || !ok {
						t.Fatalf("验签失败[%d %s pub%d]: ok=%v err=%v", i, algo, j, ok, err)
					}
				}
				// 篡改消息 -> 验签 false（不报错）
				ok, err := rsaVerifyHelper(t, msg+"x", sig, string(pubPKCS1), algo)
				if err != nil || ok {
					t.Fatalf("篡改消息验签应 false: ok=%v err=%v", ok, err)
				}
			}
		}
	})
}

// helper：用表达式引擎走一轮"加密 -> 解密"（PEM 经 $static 变量注入避免字符串转义地狱）
func rsaDecryptOAEPHelper(t *testing.T, priv *rsa.PrivateKey, pubPEM, plain string) (string, error) {
	t.Helper()
	ct, err := EvalWithVars(`$rsaEncrypt($plain, $pub)`, nil, map[string]interface{}{
		"plain": plain,
		"pub":   pubPEM,
	})
	if err != nil {
		return "", err
	}
	pt, err := EvalWithVars(`$rsaDecrypt($ct, $priv)`, nil, map[string]interface{}{
		"ct":   ct,
		"priv": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}),
	})
	if err != nil {
		return "", err
	}
	return pt.(string), nil
}

func rsaSignHelper(t *testing.T, msg, privPEM, algo string) (string, error) {
	t.Helper()
	v, err := EvalWithVars(`$rsaSign($msg, $priv, $algo)`, nil, map[string]interface{}{
		"msg": msg, "priv": privPEM, "algo": algo,
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func rsaVerifyHelper(t *testing.T, msg, sig, pubPEM, algo string) (bool, error) {
	t.Helper()
	v, err := EvalWithVars(`$rsaVerify($msg, $sig, $pub, $algo)`, nil, map[string]interface{}{
		"msg": msg, "sig": sig, "pub": pubPEM, "algo": algo,
	})
	if err != nil {
		return false, err
	}
	return v.(bool), nil
}

// 4.3 PEM 解析与错误分支
func TestRSAErrorBranches(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}))
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&priv.PublicKey)}))

	t.Run("坏 PEM 报错", func(t *testing.T) {
		if _, err := Eval(`$rsaEncrypt('x','not a pem')`, nil); err == nil || !strings.Contains(err.Error(), "PEM") {
			t.Fatalf("坏公钥应报 PEM 错误: %v", err)
		}
		if _, err := Eval(`$rsaSign('x','not a pem','sha256')`, nil); err == nil || !strings.Contains(err.Error(), "PEM") {
			t.Fatalf("坏私钥应报 PEM 错误: %v", err)
		}
		// 类型不匹配：私钥位置给了公钥
		if _, err := EvalWithVars(`$rsaSign('x',$pub,'sha256')`, nil, map[string]interface{}{"pub": pubPEM}); err == nil || !strings.Contains(err.Error(), "私钥") {
			t.Fatalf("PEM 类型不匹配应报错: %v", err)
		}
	})
	t.Run("非法算法", func(t *testing.T) {
		err := mustEvalErr(t, `$rsaSign('x','`+privPEM+`','md5')`, nil)
		if !strings.Contains(err.Error(), "仅支持 sha1/sha256") {
			t.Fatalf("非法算法错误信息: %v", err)
		}
	})
	t.Run("密钥与密文不匹配", func(t *testing.T) {
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		otherPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(other)}))
		ct, err := EvalWithVars(`$rsaEncrypt($p, $pub)`, nil, map[string]interface{}{"p": "secret", "pub": pubPEM})
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		_, err = EvalWithVars(`$rsaDecrypt($ct, $other)`, nil, map[string]interface{}{"ct": ct, "other": otherPEM})
		if err == nil || !strings.Contains(err.Error(), "解密失败") {
			t.Fatalf("密钥不匹配应报可读错误: %v", err)
		}
	})
	t.Run("PKCS#8 私钥签名可用", func(t *testing.T) {
		b, _ := x509.MarshalPKCS8PrivateKey(priv)
		p8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
		sig, err := rsaSignHelper(t, "m", string(p8), "sha256")
		if err != nil || sig == "" {
			t.Fatalf("PKCS#8 签名失败: %v", err)
		}
	})
}

// 签名场景排序工具：$sortKeys(obj)
func TestExtSortKeys(t *testing.T) {
	t.Run("字典序（码点序：大写先于小写）", func(t *testing.T) {
		obj := map[string]interface{}{"z": 1, "a": 2, "B": 3, "m": 4}
		v := mustEval(t, `$sortKeys(obj)`, map[string]interface{}{"obj": obj})
		got := v.([]string)
		want := []string{"B", "a", "m", "z"} // ASCII 字节序：B(66) < a(97) < m(109) < z(122)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sortKeys got %v, want %v", got, want)
		}
	})

	t.Run("空对象", func(t *testing.T) {
		v := mustEval(t, `$sortKeys({})`, nil)
		got := v.([]string)
		if len(got) != 0 {
			t.Fatalf("空对象应返回空数组: %v", got)
		}
	})

	t.Run("签名 canonical query 典型拼装", func(t *testing.T) {
		// 签名规范要求的组合用法：按键排序 -> k=v 对 -> & 连接
		obj := map[string]interface{}{"SignatureNonce": "n1", "Action": "Describe", "Version": "2020-01-01", "z": "zz"}
		v := mustEval(t, `$join($map($sortKeys(obj), function($f){ $f & "=" & $string($lookup(obj, $f)) }), "&")`, map[string]interface{}{"obj": obj})
		if v != "Action=Describe&SignatureNonce=n1&Version=2020-01-01&z=zz" {
			t.Fatalf("canonical query 拼装错误: %v", v)
		}
	})

	t.Run("与内置 $sort 的关系：sortKeys 一步代替 sort+keys", func(t *testing.T) {
		obj := map[string]interface{}{"b": 1, "a": 2}
		a := mustEval(t, `$sortKeys(obj)`, map[string]interface{}{"obj": obj}).([]string)
		b := mustEval(t, `$sort($keys(obj))`, map[string]interface{}{"obj": obj}).([]interface{})
		if strings.Join(a, ",") != fmt.Sprintf("%s,%s", b[0], b[1]) {
			t.Fatalf("sortKeys 应与 sort(keys) 等价: %v vs %v", a, b)
		}
	})

	t.Run("sortKeys 在可用函数集合中", func(t *testing.T) {
		if _, ok := Exts()["sortKeys"]; !ok {
			t.Fatal("sortKeys 未注册")
		}
	})
}
