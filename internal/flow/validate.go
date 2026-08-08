package flow

import (
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/jsonata"
)

// Level discriminates blocking errors from non-blocking warnings.
type Level string

const (
	LevelError   Level = "error"
	LevelWarning Level = "warning"
)

// ValidationError is one structured finding from whole-tree validation. The
// ExpectedFormat field carries the corrected shape so tool callers (e.g. the
// LLM agent) can fix the tree.
type ValidationError struct {
	NodeID         string `json:"node_id,omitempty"`
	Code           string `json:"code"`
	Level          Level  `json:"level"`
	Message        string `json:"message"`
	ExpectedFormat string `json:"expected_format,omitempty"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ValidatorOptions lets callers inject facts that are not part of the tree
// itself, such as the soft-delete state of referenced test units.
type ValidatorOptions struct {
	// UnitDeleted reports whether the referenced test unit is soft-deleted.
	// When nil, the soft-delete warning is skipped (pure structural mode).
	UnitDeleted func(unitID uint) bool
}

// Result is the outcome of whole-tree validation.
type Result struct {
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

// HasErrors reports whether any blocking error was found.
func (r Result) HasErrors() bool { return len(r.Errors) > 0 }

// Validate runs whole-tree validation over the tree.
func Validate(t *Tree, opts ValidatorOptions) Result {
	// 初始化空 slice 而非 nil：保证 JSON 序列化输出 [] 而非 null，
	// 避免前端对 errors/warnings 的 .length 访问崩溃。
	res := Result{Errors: []ValidationError{}, Warnings: []ValidationError{}}
	for _, e := range t.ValidateTreeShape() {
		res.Errors = append(res.Errors, e)
	}
	res.Errors = append(res.Errors, t.validateIOContracts()...)
	res.Errors = append(res.Errors, t.validateAdapters()...)
	res.Errors = append(res.Errors, t.validateTryCatch()...)
	res.Errors = append(res.Errors, t.validateLoops()...)
	res.Warnings = append(res.Warnings, t.validateSoftDeletedUnits(opts)...)
	return res
}

// cacheWrites maps a cache key to the set of cache-set node IDs that write it.
type cacheWrites map[string][]string

// collectCacheWrites statically gathers every cache key written by any
// cache-set node in the tree. cache-set nodes are off-link side channels, so
// visibility is whole-tree static: a referenced key must be written by some
// cache-set node (ordering is an execution-engine concern).
func (t *Tree) collectCacheWrites() cacheWrites {
	writes := cacheWrites{}
	for _, id := range t.CacheSets {
		n, ok := t.Nodes[id]
		if !ok || n == nil || n.Type != NodeCacheSet {
			continue
		}
		for _, k := range cacheSetWrites(n) {
			writes[k] = append(writes[k], id)
		}
	}
	return writes
}

// cacheSetWrites returns the keys a cache-set node writes.
func cacheSetWrites(n *Node) []string {
	var cfg struct {
		Writes map[string]string `json:"writes"`
	}
	_ = UnmarshalConfig(n, &cfg)
	var keys []string
	for k := range cfg.Writes {
		keys = append(keys, k)
	}
	return keys
}

// validateIOContracts checks that every mandatory input key of every node is
// satisfied by an ancestor's output or by a shared-cache key written by a
// cache-set reachable earlier in the tree (static DFS visibility).
func (t *Tree) validateIOContracts() []ValidationError {
	var errs []ValidationError
	for id, n := range t.Nodes {
		if n == nil {
			continue
		}
		// Outputs available from ancestors (excluding the node itself).
		provided := map[string]IOKey{}
		for _, a := range t.Ancestors(id) {
			an, ok := t.Nodes[a]
			if !ok || an == nil {
				continue
			}
			for k, v := range an.Outputs {
				provided[k] = v
			}
		}
		allCache := t.collectCacheWrites()
		for key, io := range n.Inputs {
			if io.Source != "" {
				if ck, ok := CacheKey(io.Source); ok {
					if _, exists := allCache[ck]; !exists {
						errs = append(errs, ValidationError{
							NodeID: id, Code: "contract.cache_source_missing",
							Level: LevelError, Message: fmt.Sprintf("输入 %s 引用的缓存 key %s 无 cache-set 写入者", key, ck),
							ExpectedFormat: "$cache.<key> 需存在写入该 key 的 cache-set 节点",
						})
					}
					continue
				}
				if _, exists := provided[io.Source]; !exists {
					errs = append(errs, ValidationError{
						NodeID: id, Code: "contract.source_missing",
						Level: LevelError, Message: fmt.Sprintf("输入 %s 引用的上游输出 %s 不存在", key, io.Source),
						ExpectedFormat: "来源需为祖先节点输出的键,或 $cache.<key> 形式",
					})
				}
				continue
			}
			if _, exists := provided[key]; exists {
				continue
			}
			if _, exists := allCache[key]; exists {
				continue
			}
			if isAuthKey(key) {
				errs = append(errs, ValidationError{
					NodeID: id, Code: "contract.auth_source_missing",
					Level: LevelError, Message: fmt.Sprintf("认证键 %s 缺少来源,需由上游输出或共享缓存提供", key),
					ExpectedFormat: "认证键需由上游 api 输出(如登录 token)或 cache-set 写入",
				})
				continue
			}
			errs = append(errs, ValidationError{
				NodeID: id, Code: "contract.input_unresolved",
				Level: LevelError, Message: fmt.Sprintf("输入 %s 无来源", key),
				ExpectedFormat: "需由祖先节点输出或缓存 key 满足",
			})
		}
	}
	return errs
}

// isAuthKey recognizes header authentication keys such as authorization or token.
func isAuthKey(key string) bool {
	lk := strings.ToLower(key)
	for _, token := range []string{"authorization", "token", "api-key", "apikey", "cookie", "auth"} {
		if strings.Contains(lk, token) {
			return true
		}
	}
	return false
}

// validateAdapters checks the JSONata syntax of adapter nodes.
func (t *Tree) validateAdapters() []ValidationError {
	var errs []ValidationError
	for id, n := range t.Nodes {
		if n == nil || n.Type != NodeAdapter {
			continue
		}
		var cfg struct {
			Expr string `json:"expr"`
		}
		if err := UnmarshalConfig(n, &cfg); err != nil {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "adapter.config_invalid", Level: LevelError,
				Message: "adapter 配置无法解析", ExpectedFormat: `{"expr": "<jsonata>"}`,
			})
			continue
		}
		if err := jsonata.Parse(cfg.Expr); err != nil {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "adapter.jsonata_invalid", Level: LevelError,
				Message:        "JSONata 表达式语法错误: " + err.Error(),
				ExpectedFormat: "合法的 JSONata 表达式",
			})
		}
	}
	return errs
}

// validateTryCatch checks catch placement per-branch: a catch must lie inside
// a try scope (its nearest try), and a catch attached directly under a try
// (a sibling catch branch) must come after at least one executable branch so
// it can actually consume a failure. A catch buried before every branch or
// outside any try is rejected.
func (t *Tree) validateTryCatch() []ValidationError {
	var errs []ValidationError
	for id, n := range t.Nodes {
		if n == nil {
			continue
		}
		if n.Type == NodeCatch {
			tryID := t.nearestTryOf(id)
			if tryID == "" {
				errs = append(errs, ValidationError{
					NodeID: id, Code: "trycatch.outside_try", Level: LevelError,
					Message:        "catch 节点不在任何 try 作用域内",
					ExpectedFormat: "catch 需挂在某 try 子树内",
				})
				continue
			}
			// A catch directly under a try must not precede every executable
			// branch: it would have no failure to consume (claims a scope it
			// cannot serve).
			if n.Parent == tryID && !t.catchPrecedesExecutable(tryID, id) {
				errs = append(errs, ValidationError{
					NodeID: id, Code: "trycatch.catch_unreachable", Level: LevelError,
					Message:        "catch 位于 try 首部,前面没有任何可执行分支可供其捕获失败",
					ExpectedFormat: "把 catch 放到 try 的某个可执行分支之后,或挂到具体分支子树下",
				})
			}
		}
		if n.Type == NodeTry {
			if t.hasDescendantOfType(id, NodeCatch) && !t.hasDescendantOfType(id, NodeAPI) && !t.hasDescendantOfType(id, NodeAdapter) && !t.hasDescendantOfType(id, NodeAssert) {
				errs = append(errs, ValidationError{
					NodeID: id, Code: "trycatch.empty_scope", Level: LevelError,
					Message:        "try 作用域为空",
					ExpectedFormat: "try 子树内至少有一个执行节点",
				})
			}
		}
	}
	return errs
}

// nearestTryOf returns the id of the closest enclosing try node of id, or
// empty when none exists.
func (t *Tree) nearestTryOf(id string) string {
	for _, a := range t.Ancestors(id) {
		if n, ok := t.Nodes[a]; ok && n != nil && n.Type == NodeTry {
			return a
		}
	}
	return ""
}

// catchPrecedesExecutable reports whether the sibling catch at catchID under
// tryID follows at least one executable branch (api/adapter/assert/loop or a
// nested try) among the try's children, so it can consume a pending failure.
func (t *Tree) catchPrecedesExecutable(tryID, catchID string) bool {
	tr, ok := t.Nodes[tryID]
	if !ok || tr == nil {
		return false
	}
	for _, child := range tr.Children {
		if child == catchID {
			return false
		}
		cn, ok := t.Nodes[child]
		if !ok || cn == nil {
			continue
		}
		switch cn.Type {
		case NodeAPI, NodeAdapter, NodeAssert, NodeLoop, NodeTry:
			return true
		}
	}
	return false
}

// validateLoops checks that loop nodes declare an array-valued input.
func (t *Tree) validateLoops() []ValidationError {
	var errs []ValidationError
	for id, n := range t.Nodes {
		if n == nil || n.Type != NodeLoop {
			continue
		}
		var cfg struct {
			Input string `json:"input"`
			Var   string `json:"var"`
		}
		if err := UnmarshalConfig(n, &cfg); err != nil {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "loop.config_invalid", Level: LevelError,
				Message: "loop 配置无法解析", ExpectedFormat: `{"input": "<array key>", "var": "<item>"}`,
			})
			continue
		}
		if cfg.Input == "" || cfg.Var == "" {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "loop.missing_fields", Level: LevelError,
				Message:        "loop 需要 input 与 var 字段",
				ExpectedFormat: `{"input": "<array key>", "var": "<item>"}`,
			})
			continue
		}
		io, ok := n.Inputs[cfg.Input]
		if !ok {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "loop.input_not_declared", Level: LevelError,
				Message:        fmt.Sprintf("loop 输入 %s 未在 inputs 中声明", cfg.Input),
				ExpectedFormat: "loop 的 input 键需在 inputs 中声明且类型为 array",
			})
			continue
		}
		if io.Type != IOTypeArray {
			errs = append(errs, ValidationError{
				NodeID: id, Code: "loop.input_not_array", Level: LevelError,
				Message:        fmt.Sprintf("loop 输入 %s 类型为 %s,需为 array", cfg.Input, io.Type),
				ExpectedFormat: "loop 输入键类型需为 array",
			})
		}
	}
	return errs
}

// validateSoftDeletedUnits emits a warning when an api node references a
// soft-deleted test unit; the snapshot still renders.
func (t *Tree) validateSoftDeletedUnits(opts ValidatorOptions) []ValidationError {
	if opts.UnitDeleted == nil {
		return nil
	}
	var warnings []ValidationError
	for id, n := range t.Nodes {
		if n == nil || n.Type != NodeAPI {
			continue
		}
		var cfg struct {
			UnitID uint `json:"unit_id"`
		}
		if err := UnmarshalConfig(n, &cfg); err != nil || cfg.UnitID == 0 {
			continue
		}
		if opts.UnitDeleted(cfg.UnitID) {
			warnings = append(warnings, ValidationError{
				NodeID: id, Code: "api.unit_soft_deleted", Level: LevelWarning,
				Message:        "引用的测试单元已软删,当前使用版本内快照",
				ExpectedFormat: "无需修复;提示用户单元已变更",
			})
		}
	}
	return warnings
}

func (t *Tree) hasDescendantOfType(id string, typ NodeType) bool {
	for nid, n := range t.Nodes {
		if n == nil || nid == id {
			continue
		}
		if n.Type == typ && t.IsDescendant(id, nid) {
			return true
		}
	}
	return false
}
