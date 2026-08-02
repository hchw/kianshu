// Package jsonata wraps the Go JSONata engine with project conventions:
// syntax checking, evaluation, and the common encryption-suite extension
// functions used by adapter and start nodes.
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

// Parse compiles an expression and returns an error for invalid syntax.
func Parse(expr string) error {
	if expr == "" {
		return fmt.Errorf("表达式不能为空")
	}
	_, err := jsonata.Compile(expr)
	return err
}

// Eval evaluates a JSONata expression against the given input data.
func Eval(expr string, data any) (any, error) {
	e, err := jsonata.Compile(expr)
	if err != nil {
		return nil, err
	}
	registerExts(e)
	return e.Eval(data)
}

// Exts returns the encryption-suite extension functions, shared by all
// expression evaluations. Names are registered without the leading '$'.
func Exts() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		"base64encode": {Func: func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		}},
		"base64decode": {Func: func(s string) (string, error) {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}},
		"md5": {Func: func(s string) string {
			h := md5.Sum([]byte(s))
			return fmt.Sprintf("%x", h)
		}},
		"sha1": {Func: func(s string) string {
			h := sha1.Sum([]byte(s))
			return fmt.Sprintf("%x", h)
		}},
		"sha256": {Func: func(s string) string {
			h := sha256.Sum256([]byte(s))
			return fmt.Sprintf("%x", h)
		}},
		"hmac": {Func: func(secret, msg, algo string) (string, error) {
			var h func() hash.Hash
			switch algo {
			case "sha256":
				h = sha256.New
			case "sha1":
				h = sha1.New
			case "md5":
				h = md5.New
			default:
				return "", fmt.Errorf("不支持的 hmac 算法: %s", algo)
			}
			mac := hmac.New(h, []byte(secret))
			mac.Write([]byte(msg))
			return fmt.Sprintf("%x", mac.Sum(nil)), nil
		}},
	}
}

func registerExts(e *jsonata.Expr) {
	if err := e.RegisterExts(Exts()); err != nil {
		panic("register jsonata extensions: " + err.Error())
	}
}
