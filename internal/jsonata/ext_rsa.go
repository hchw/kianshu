package jsonata

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"github.com/xiatechs/jsonata-go"
)

// --------------------------------------------
// RSA 非对称加解密 / 签名扩展（D1/D2）。
//
// - 加密固定使用 OAEP（哈希 SHA-256），返回 base64 密文；
// - 签名算法经 algo 指定：sha1 | sha256，签名为 base64；验签返回布尔值；
// - PEM 解析同时支持 PKCS#1（RSA PRIVATE KEY / RSA PUBLIC KEY）与
//   PKCS#8（PRIVATE KEY / PUBLIC KEY）两种包装格式；
// - 公钥/私钥不匹配或密文损坏时返回可读错误，验签失败返回 false（不报错）。
// --------------------------------------------

func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("rsa 私钥 PEM 解析失败: 未找到合法 PEM 块")
	}
	switch block.Type {
	case "RSA PRIVATE KEY": // PKCS#1
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("rsa PKCS#1 私钥解析失败: %v", err)
		}
		return k, nil
	case "PRIVATE KEY": // PKCS#8
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("rsa PKCS#8 私钥解析失败: %v", err)
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("rsa PKCS#8 私钥不是 RSA 类型")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("rsa 私钥 PEM 类型不支持: %s（支持 RSA PRIVATE KEY / PRIVATE KEY）", block.Type)
	}
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("rsa 公钥 PEM 解析失败: 未找到合法 PEM 块")
	}
	switch block.Type {
	case "RSA PUBLIC KEY": // PKCS#1
		k, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("rsa PKCS#1 公钥解析失败: %v", err)
		}
		return k, nil
	case "PUBLIC KEY": // PKIX/SPKI（PKCS#8 公钥包装）
		k, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("rsa PKIX 公钥解析失败: %v", err)
		}
		rsaKey, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("rsa 公钥不是 RSA 类型")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("rsa 公钥 PEM 类型不支持: %s（支持 RSA PUBLIC KEY / PUBLIC KEY）", block.Type)
	}
}

// rsaSignHash 解析签名算法并返回对应签名字节。
func rsaSignHash(algo string) (crypto.Hash, error) {
	switch algo {
	case "sha1":
		return crypto.SHA1, nil
	case "sha256":
		return crypto.SHA256, nil
	default:
		return 0, fmt.Errorf("不支持的 rsa 签名算法: %s（仅支持 sha1/sha256）", algo)
	}
}

func hashMessageBytes(algo crypto.Hash, msg []byte) []byte {
	switch algo {
	case crypto.SHA1:
		h := sha1.Sum(msg)
		return h[:]
	default:
		h := sha256.Sum256(msg)
		return h[:]
	}
}

// extsRSA 注册 $rsaEncrypt / $rsaDecrypt / $rsaSign / $rsaVerify。
func extsRSA() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		// $rsaEncrypt(plain, pubPEM) -> base64 密文（OAEP-SHA256）
		"rsaEncrypt": {Func: func(plain, pubPEM string) (string, error) {
			pub, err := parseRSAPublicKey(pubPEM)
			if err != nil {
				return "", err
			}
			out, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, []byte(plain), nil)
			if err != nil {
				return "", fmt.Errorf("rsa 加密失败: %v", err)
			}
			return base64.StdEncoding.EncodeToString(out), nil
		}},
		// $rsaDecrypt(cipherB64, privPEM) -> 明文
		"rsaDecrypt": {Func: func(cipherB64, privPEM string) (string, error) {
			priv, err := parseRSAPrivateKey(privPEM)
			if err != nil {
				return "", err
			}
			ct, err := base64.StdEncoding.DecodeString(cipherB64)
			if err != nil {
				return "", fmt.Errorf("rsa 密文 base64 解码失败: %v", err)
			}
			out, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, ct, nil)
			if err != nil {
				return "", fmt.Errorf("rsa 解密失败（密文损坏或密钥不匹配）: %v", err)
			}
			return string(out), nil
		}},
		// $rsaSign(msg, privPEM, algo) -> base64 签名（algo: sha1|sha256）
		"rsaSign": {Func: func(msg, privPEM, algo string) (string, error) {
			priv, err := parseRSAPrivateKey(privPEM)
			if err != nil {
				return "", err
			}
			hashID, err := rsaSignHash(algo)
			if err != nil {
				return "", err
			}
			digest := hashMessageBytes(hashID, []byte(msg))
			sig, err := rsa.SignPKCS1v15(rand.Reader, priv, hashID, digest)
			if err != nil {
				return "", fmt.Errorf("rsa 签名失败: %v", err)
			}
			return base64.StdEncoding.EncodeToString(sig), nil
		}},
		// $rsaVerify(msg, sigB64, pubPEM, algo) -> true/false（参数错误时报错，验签失败返回 false）
		"rsaVerify": {Func: func(msg, sigB64, pubPEM, algo string) (bool, error) {
			pub, err := parseRSAPublicKey(pubPEM)
			if err != nil {
				return false, err
			}
			hashID, err := rsaSignHash(algo)
			if err != nil {
				return false, err
			}
			sig, err := base64.StdEncoding.DecodeString(sigB64)
			if err != nil {
				return false, fmt.Errorf("rsa 签名 base64 解码失败: %v", err)
			}
			digest := hashMessageBytes(hashID, []byte(msg))
			if err := rsa.VerifyPKCS1v15(pub, hashID, digest, sig); err != nil {
				return false, nil
			}
			return true, nil
		}},
	}
}
