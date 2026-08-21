package flow

import (
	"reflect"
	"strings"
	"testing"

	"github/hchw/kianshu/internal/jsonata"
)

// 6.1 词法扫描器
func TestScanFuncCalls(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		// 普通函数调用
		{`$uppercase(name)`, []string{"uppercase"}},
		// 多个调用 + 嵌套
		{`$hmacb64($hexdecode('ab'), $sha256(x), 'sha256')`, []string{"hmacb64", "hexdecode", "sha256"}},
		// $ 后跟名字但不跟 ( ：变量引用，不算函数
		{`$static.token`, nil},
		// 裸 $、$.
		{`$ .body`, nil},
		{`$..body`, nil},
		// $ 结尾 / 空
		{`foo $`, nil},
		{``, nil},
		// 变量与函数混用：变量名不进清单
		{`$hmacb64($secret, $msg.raw, 'sha1')`, []string{"hmacb64"}},
		// 名字后跟空格再括号也算调用
		{`$count (items)`, []string{"count"}},
		// 字符串字面量里的 $ 不误报：字符串内 'a$foo(b)' 是普通文本
		{`$contains(msg, 'a$foo(b)')`, []string{"contains"}},
		// 大小写敏感：名字原样保留
		{`$Hmacb64('a','b','c')`, []string{"Hmacb64"}},
	}
	for _, c := range cases {
		got := ScanFuncCalls(c.expr)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ScanFuncCalls(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestUnknownFuncFindings(t *testing.T) {
	// 拼错的扩展名 -> 命中
	got := UnknownFuncFindings(`$hmac64('a','b')`)
	if !reflect.DeepEqual(got, []string{"hmac64"}) {
		t.Fatalf("unknown: %v", got)
	}
	// 内置 + 扩展 + 变量引用 -> 零误报
	expr := `$count($filter($.items, $f => $f.ok)) & $hmacb64($secret, $msg, 'sha256') & $aesEncrypt('x', $k, $iv, 'cbc')`
	if got := UnknownFuncFindings(expr); len(got) != 0 {
		t.Fatalf("不应有未知函数: %v", got)
	}
	// 完全正常表达式
	if got := UnknownFuncFindings(`$.body.token`); len(got) != 0 {
		t.Fatalf("变量引用不应报未知: %v", got)
	}
}

// 6.2 内置清单防漂移：清单里每个名字都能求值调用（库升级改名/删名即失败）
func TestBuiltinFuncNamesCallable(t *testing.T) {
	for _, name := range jsonata.BuiltinFuncNames() {
		_, err := jsonata.Eval("$"+name+"(0)", nil)
		if err != nil && strings.Contains(err.Error(), "non-function") {
			t.Errorf("清单中的内置函数 $%s 已不可调用（库升级漂移？）: %v", name, err)
		}
	}
	// 扩展函数全部在可用函数集合中（Exts 动态并入）
	for name := range jsonata.Exts() {
		if !funcExists(name) {
			t.Errorf("扩展函数 $%s 不在可用集合中", name)
		}
	}
	// 可用清单非空且含内置与扩展代表
	list := availableFuncNames()
	if !contains(list, "uppercase") || !contains(list, "hmacb64") || !contains(list, "rsaSign") {
		t.Fatalf("可用清单缺内置/扩展代表: %v", list)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// 6.3 validateAdapters 接入：未知函数 warning、不误报内置
func TestValidateAdapterUnknownFuncWarns(t *testing.T) {
	tree := helperTree(t, func(tree *Tree) {
		tree.Start = "n1"
		n1 := add(tree, "n1", NodeAdapter)
		n1.Config = mustConfig(map[string]any{"expr": "$hmac64('a','b')"})
	})
	res := Validate(tree, ValidatorOptions{})
	if res.HasErrors() {
		t.Fatalf("未知函数不应产生 error: %v", res.Errors)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("应有 1 条 warning, got %v", res.Warnings)
	}
	w := res.Warnings[0]
	if w.Code != "adapter.unknown_func" || w.Level != LevelWarning {
		t.Fatalf("warning 形状不对: %+v", w)
	}
	if !strings.Contains(w.Message, "hmac64") || !strings.Contains(w.Message, "hmacb64") {
		t.Fatalf("warning 应含未知名与可用函数列表: %s", w.Message)
	}

	// 合法表达式：内置 + 扩展 + 变量引用 -> 零 warning
	ok := helperTree(t, func(tree *Tree) {
		tree.Start = "n1"
		n1 := add(tree, "n1", NodeAdapter)
		n1.Config = mustConfig(map[string]any{"expr": "$count($.items) & $hmacb64($k, $m, 'sha256') & $sortKeys($.params)"})
		n2 := add(tree, "n2", NodeAdapter)
		n2.Config = mustConfig(map[string]any{"expr": "$uppercase($static.x)"})
		link(tree, "n1", "n2")
	})
	res2 := Validate(ok, ValidatorOptions{})
	if len(res2.Warnings) != 0 {
		t.Fatalf("合法表达式不应有 warning: %v", res2.Warnings)
	}
}
