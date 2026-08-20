package flow

import (
	"sort"

	"github/hchw/kianshu/internal/jsonata"
)

// adapterFuncCheck 静态识别 adapter 表达式中的函数调用（D5）。
//
// 背景：xiatechs/jsonata-go 中函数与变量共用同一命名空间，编译期不区分、
// 不报错；未注册/拼错的函数名直到运行期才报 ErrNonCallable。唯一可靠的
// 静态区分信号是词法形态。本包只做"函数存在性"检查（warning 级），
// 不做完整求值语义校验。
//
// 词法规则：
//   - `$name(`（$ 后跟标识符且紧随 `(`）视为函数调用，名字进入检查；
//   - 裸 `$`、`$.`、`$static`、`$name` 后不跟 `(` 的视为变量/上下文引用，
//     不检查；
//   - 函数名大小写敏感（库内置与扩展注册均精确匹配）。

// builtinFuncSet 与 extFuncSet 合并为可用函数集合：
//   - 内置：jsonata.BuiltinFuncNames()（单一来源，含防漂移测试）；
//   - 扩展：jsonata.Exts() 动态并入。
var builtinFuncSet = func() map[string]bool {
	m := make(map[string]bool, 71)
	for _, n := range jsonata.BuiltinFuncNames() {
		m[n] = true
	}
	return m
}()

// extFuncSet 构建一次：内置清单 ∪ 扩展函数（每次调用重新读 Exts() 成本可忽略，
// 但为稳定性缓存一次构建结果——由测试保证扩展集合与运行期一致）。
var extFuncSet = func() map[string]bool {
	m := make(map[string]bool)
	for name := range jsonata.Exts() {
		m[name] = true
	}
	return m
}()

// funcExists 报告名字是否为可用函数（内置或扩展，精确匹配）。
func funcExists(name string) bool {
	return builtinFuncSet[name] || extFuncSet[name]
}

// ScanFuncCalls 词法扫描表达式中的函数调用名，返回去重后的名字列表
// （保留原始大小写，顺序稳定）。纯函数，可单测。
func ScanFuncCalls(expr string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	i := 0
	for i < len(expr) {
		c := expr[i]
		// 跳过字符串字面量（单/双引号），字符串内的 $name( 不是函数调用
		if c == '\'' || c == '"' {
			i = skipStringLiteral(expr, i)
			continue
		}
		if c != '$' {
			i++
			continue
		}
		// $ 后必须是标识符起始（字母或下划线）；$. / $ 结尾等是变量引用
		j := i + 1
		if j >= len(expr) || !isIdentStart(expr[j]) {
			i++
			continue
		}
		k := j
		for k < len(expr) && isIdentPart(expr[k]) {
			k++
		}
		name := expr[j:k]
		// 名字后（允许空白）紧跟 ( 才视为函数调用
		l := k
		for l < len(expr) && (expr[l] == ' ' || expr[l] == '\t' || expr[l] == '\n') {
			l++
		}
		if l < len(expr) && expr[l] == '(' {
			add(name)
		}
		i = k
	}
	return names
}

func skipStringLiteral(s string, start int) int {
	quote := s[start]
	i := start + 1
	for i < len(s) {
		if quote == '"' && s[i] == '\\' {
			i += 2 // 跳过 \x 转义
			continue
		}
		if s[i] == quote {
			if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				i += 2 // '' 转义的单引号
				continue
			}
			return i + 1
		}
		i++
	}
	return len(s)
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// UnknownFuncFindings 返回表达式中引用的、不在可用函数集合内的函数名
// （去重、排序，便于 diff）。变量/上下文引用不会被当作函数。
func UnknownFuncFindings(expr string) []string {
	var unknown []string
	seen := map[string]bool{}
	for _, name := range ScanFuncCalls(expr) {
		if funcExists(name) || seen[name] {
			continue
		}
		seen[name] = true
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	return unknown
}

// availableFuncNames 返回"内置 ∪ 扩展"的完整函数名列表（warning 文案附带）。
func availableFuncNames() []string {
	all := make([]string, 0, len(builtinFuncSet)+len(extFuncSet))
	for n := range builtinFuncSet {
		all = append(all, n)
	}
	for n := range extFuncSet {
		all = append(all, n)
	}
	sort.Strings(all)
	return all
}
