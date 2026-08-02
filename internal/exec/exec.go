// Package exec implements the runtime executor of a test flow's execution
// tree: depth-first tree walking, failure containment (a failing node stops
// only its own subtree while siblings continue), try/catch and loop
// semantics, and a run-scoped shared cache. The executor is pure logic with
// injected dependencies (HTTP calls, start parameters) so it can be
// unit-tested and reused by the HTTP layer and the future LLM agent.
package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/jsonata"
)

// Status is the outcome of a node or of a whole run.
type Status string

const (
	// StatusOK means the node/run completed successfully.
	StatusOK Status = "ok"
	// StatusFailed means the node/run failed hard (HTTP error, assertion,
	// adapter evaluation error, ...).
	StatusFailed Status = "failed"
	// StatusSoftStop means the node/run stopped softly (a try scope without a
	// catch, a failed loop iteration); siblings continue.
	StatusSoftStop Status = "soft-stop"
)

// APICall describes one outbound HTTP call performed by an api node.
type APICall struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string]any
	Body    any
}

// NodeResult records the outcome of a single node execution.
type NodeResult struct {
	NodeID     string    `json:"node_id"`
	Status     Status    `json:"status"`
	Input      any       `json:"input,omitempty"`
	Output     any       `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// Options carries the injected dependencies of a run.
type Options struct {
	// StartParams are the initial inputs produced by the start node. When
	// empty the start node's own config params are used.
	StartParams map[string]any
	// Host is the test set's target host used to build api node URLs. It is
	// taken at execution time from the test set, never from the version.
	Host string
	// CallAPI performs an outbound HTTP call for an api node. When nil, api
	// nodes fail with an explanatory error (pure model execution).
	CallAPI func(APICall) (any, error)
	// Timeout bounds a single HTTP call. Zero means no explicit timeout.
	Timeout time.Duration
}

// RunResult is the outcome of a whole-tree run.
type RunResult struct {
	Results map[string]*NodeResult `json:"results"`
	Cache   map[string]any         `json:"cache,omitempty"`
	Status  Status                 `json:"status"`
}

// Run executes the whole tree once with a fresh run-scoped cache. The start
// node runs first, then the start node's subtrees are walked depth-first.
// Cache-sets are off-link side channels: one executes lazily the moment a
// node first reads a key it writes (evaluating its writes against the
// reading node's parent output), so the design's token chain
// (login api → cache-set → downstream $cache read) works; any cache-set
// never read runs at the end with the start output as input so every
// cache-set has a result. ctx may carry a deadline that bounds the whole run
// (checked between nodes so non-network work also respects it).
func Run(ctx context.Context, t *flow.Tree, opts Options) (*RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || t.Nodes == nil {
		return nil, fmt.Errorf("执行树为空")
	}
	start, ok := t.Nodes[t.Start]
	if !ok || start == nil {
		return nil, fmt.Errorf("缺少 start 根节点")
	}
	e := &engine{
		tree:       t,
		opts:       opts,
		cache:      map[string]any{},
		results:    map[string]*NodeResult{},
		cacheSetRan: map[string]bool{},
		doneCatch:  map[string]bool{},
		ctx:        ctx,
	}
	startOut, startErr := e.execute(start, nil)
	if startErr != nil {
		e.record(t.Start, StatusFailed, nil, nil, startErr)
		e.rootStatus = StatusFailed
		return &RunResult{Results: e.results, Cache: e.cache, Status: e.rootStatus}, nil
	}
	e.record(t.Start, StatusOK, nil, startOut, nil)
	status := StatusOK
	for _, child := range start.Children {
		if e.runNode(child, startOut) == StatusFailed {
			status = StatusFailed
		}
	}
	// Run any cache-set that no node read during the walk, so it still
	// produces its full-cache result.
	for _, csID := range t.CacheSets {
		if e.cacheSetRan[csID] {
			continue
		}
		cs, ok := t.Nodes[csID]
		if !ok || cs == nil || cs.Type != flow.NodeCacheSet {
			continue
		}
		in := asInputMap(startOut)
		out, err := e.execute(cs, in)
		if err != nil {
			e.record(csID, StatusFailed, in, nil, err)
			continue
		}
		e.record(csID, StatusOK, in, out, nil)
		e.cacheSetRan[csID] = true
	}
	e.rootStatus = status
	return &RunResult{Results: e.results, Cache: e.cache, Status: e.rootStatus}, nil
}

type engine struct {
	tree        *flow.Tree
	opts        Options
	cache       map[string]any
	results     map[string]*NodeResult
	iterCtx     map[string]any
	rootStatus  Status
	cacheSetRan map[string]bool
	doneCatch   map[string]bool
	ctx         context.Context
}

// contextErr returns a timeout error when the run context has expired.
func (e *engine) contextErr() error {
	if err := e.ctx.Err(); err != nil {
		return fmt.Errorf("执行超时: %w", err)
	}
	return nil
}

// runNode executes a node and its subtree, recording per-node results. The
// returned status is the subtree outcome: a hard failure propagates up so an
// enclosing try scope can handle it, while siblings of a failing node still
// run (the walk never aborts because of one branch).
func (e *engine) runNode(id string, parentOut any) Status {
	n, ok := e.tree.Nodes[id]
	if !ok || n == nil {
		return StatusFailed
	}
	// A catch already handled by execCatchFailure must not run again as a
	// pass-through: it only surfaces its fallback and its children already ran.
	if n.Type == flow.NodeCatch && e.doneCatch[id] {
		if r, ok := e.results[id]; ok {
			return r.Status
		}
		return StatusOK
	}
	// Composite nodes (try, loop) have scope-wide semantics of their own.
	if n.Type == flow.NodeTry {
		return e.runTry(n, parentOut)
	}
	if n.Type == flow.NodeLoop {
		return e.runLoop(n, parentOut)
	}
	if err := e.contextErr(); err != nil {
		e.record(id, StatusFailed, nil, nil, err)
		return StatusFailed
	}
	started := time.Now()
	input := e.resolveInputs(n, parentOut)
	out, err := e.execute(n, input)
	if err != nil {
		e.record(id, StatusFailed, input, nil, err)
		return StatusFailed
	}
	e.record(id, StatusOK, input, out, nil)
	status := StatusOK
	for _, child := range n.Children {
		if e.runNode(child, out) == StatusFailed {
			status = StatusFailed
		}
	}
	if status != StatusOK {
		e.results[id].Status = status
	}
	e.results[id].StartedAt = started
	return status
}

// execute runs a single leaf node's own logic.
func (e *engine) execute(n *flow.Node, input any) (any, error) {
	switch n.Type {
	case flow.NodeStart:
		return e.startOutput(n)
	case flow.NodeAPI:
		return e.apiOutput(n, asInputMap(input))
	case flow.NodeAdapter:
		return e.adapterOutput(n, input)
	case flow.NodeAssert:
		return e.assertOutput(n, input)
	case flow.NodeCacheSet:
		return e.cacheSetOutput(n, input)
	case flow.NodeCatch:
		// Normal path: pass through the preceding node's output unchanged.
		return input, nil
	default:
		return nil, fmt.Errorf("未知节点类型: %s", n.Type)
	}
}

// asInputMap coerces a node input into a map, returning an empty map when the
// value is not a map (e.g. a scalar adapter output consumed by an api node).
func asInputMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// record stores a node result (input/output kept opaque for inspection).
func (e *engine) record(id string, status Status, input, output any, err error) {
	r := &NodeResult{NodeID: id, Status: status, Input: input, Output: output}
	if err != nil {
		r.Error = err.Error()
	}
	e.results[id] = r
}

// startOutput produces the start node's initial parameters. When StartParams
// are provided they win; otherwise the node's config params (which may carry
// encryption-suite JSONata calls) are evaluated.
func (e *engine) startOutput(n *flow.Node) (any, error) {
	if e.opts.StartParams != nil {
		return e.opts.StartParams, nil
	}
	var cfg struct {
		Params map[string]any `json:"params"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return nil, err
	}
	if cfg.Params == nil {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	for k, v := range cfg.Params {
		ev, err := evalParamValue(v, nil)
		if err != nil {
			return nil, fmt.Errorf("start 参数 %s 求值失败: %w", k, err)
		}
		out[k] = ev
	}
	return out, nil
}

// isJSONataExpr reports whether a string should be evaluated as a JSONata
// expression (starts with '=' which is a JSONata function call).
func isJSONataExpr(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 1 && s[0] == '=' && s[1] != '='
}

// evalParamValue evaluates a param value when it is a JSONata expression
// ('='-prefixed), otherwise returns it unchanged.
func evalParamValue(v any, input map[string]any) (any, error) {
	s, ok := v.(string)
	if !ok || !isJSONataExpr(s) {
		return v, nil
	}
	expr := strings.TrimSpace(s)[1:]
	ev, err := jsonata.Eval(expr, input)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// apiOutput builds and performs the HTTP call of an api node using the
// snapshotted unit method/path, the node's user-added params, and the
// run-scoped host. Auth-ish input keys become headers; the remaining keys
// become the request body for methods that carry one, otherwise the query
// string. Explicit params override upstream values with the same key, and
// `=`-prefixed param strings are evaluated as JSONata expressions against the
// current input (mirroring start params).
func (e *engine) apiOutput(n *flow.Node, input any) (any, error) {
	in := asInputMap(input)
	if e.opts.CallAPI == nil {
		return nil, fmt.Errorf("api 节点需要注入 CallAPI")
	}
	var cfg struct {
		Unit struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"unit"`
		Params map[string]any `json:"params,omitempty"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return nil, err
	}
	method := strings.ToUpper(cfg.Unit.Method)
	if method == "" || cfg.Unit.Path == "" {
		return nil, fmt.Errorf("api 节点缺少 unit 快照(method/path)")
	}
	// Evaluate user-added params. A string starting with '=' is a JSONata
	// expression evaluated against the current input; other values are literal.
	params := map[string]any{}
	for k, v := range cfg.Params {
		ev, err := evalParamValue(v, in)
		if err != nil {
			return nil, fmt.Errorf("api 参数 %s 求值失败: %w", k, err)
		}
		params[k] = ev
	}
	// Merge input and params, explicit params taking precedence on the same key.
	merged := map[string]any{}
	for k, v := range in {
		merged[k] = v
	}
	for k, v := range params {
		merged[k] = v
	}
	path, used := substitutePath(cfg.Unit.Path, merged)
	for name := range used {
		delete(merged, name)
	}
	headers := map[string]string{}
	query := map[string]any{}
	body := map[string]any{}
	for k, v := range merged {
		switch {
		case isAuthKey(k):
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		case hasRequestBody(method):
			body[k] = v
		default:
			query[k] = v
		}
	}
	host := e.opts.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	url := strings.TrimRight(host, "/") + path
	call := APICall{Method: method, URL: url, Headers: headers, Query: query, Body: body}
	log.Printf("[exec] %s %s", method, url)
	resp, err := e.opts.CallAPI(call)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// pathParamRe matches a `{name}` placeholder in a URL path.
var pathParamRe = regexp.MustCompile(`\{([^{}]+)\}`)

// substitutePath replaces `{name}` placeholders in a path with the matching
// value from params. It returns the substituted path and the set of consumed
// placeholder names so callers can drop them from query/body routing. Unknown
// placeholders are left untouched.
func substitutePath(path string, params map[string]any) (string, map[string]bool) {
	used := map[string]bool{}
	replaced := pathParamRe.ReplaceAllStringFunc(path, func(m string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "{"), "}")
		if v, ok := params[name]; ok {
			used[name] = true
			return fmt.Sprintf("%v", v)
		}
		return m
	})
	return replaced, used
}

// hasRequestBody reports whether the HTTP method conventionally carries a
// request body.
func hasRequestBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// adapterOutput transforms its input via a JSONata expression.
func (e *engine) adapterOutput(n *flow.Node, input any) (any, error) {
	var cfg struct {
		Expr string `json:"expr"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return nil, err
	}
	if cfg.Expr == "" {
		return nil, fmt.Errorf("adapter 缺少 JSONata 表达式")
	}
	return jsonata.Eval(cfg.Expr, input)
}

// Assertion is one check performed by an assert node.
type Assertion struct {
	Field    string `json:"field"`
	Operator string `json:"op"`
	Expected any    `json:"expected"`
}

// assertOutput evaluates the node's assertions against its input and passes
// the input through unchanged when every assertion holds.
func (e *engine) assertOutput(n *flow.Node, input any) (any, error) {
	in := asInputMap(input)
	var cfg struct {
		Assertions []Assertion `json:"assertions"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return nil, err
	}
	for _, a := range cfg.Assertions {
		ok, err := e.evalAssertion(a, in)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("断言失败: %s %s %v", a.Field, a.Operator, a.Expected)
		}
	}
	return input, nil
}

// evalAssertion resolves a dotted field path and compares it with the
// expected value using the configured operator.
func (e *engine) evalAssertion(a Assertion, input map[string]any) (bool, error) {
	actual, ok := getByPath(input, a.Field)
	if !ok {
		return false, nil
	}
	switch a.Operator {
	case "eq", "==":
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", a.Expected), nil
	case "ne", "!=":
		return fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", a.Expected), nil
	case "contains":
		s, ok := actual.(string)
		if !ok {
			return false, nil
		}
		return strings.Contains(s, fmt.Sprintf("%v", a.Expected)), nil
	case "gt":
		return gt(actual, a.Expected)
	case "lt":
		return lt(actual, a.Expected)
	default:
		return false, fmt.Errorf("不支持的断言运算符: %s", a.Operator)
	}
}

// getByPath walks a dotted path into a nested map/array value.
func getByPath(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	cur := v
	for _, part := range strings.Split(path, ".") {
		switch t := cur.(type) {
		case map[string]any:
			nv, ok := t[part]
			if !ok {
				return nil, false
			}
			cur = nv
		case []any:
			var idx int
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 0 || idx >= len(t) {
				return nil, false
			}
			cur = t[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// cacheSetOutput evaluates each configured write expression against the
// current input, stores the values in the run-scoped cache, and outputs the
// entire current cache.
func (e *engine) cacheSetOutput(n *flow.Node, input any) (any, error) {
	var cfg struct {
		Writes map[string]string `json:"writes"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return nil, err
	}
	for k, expr := range cfg.Writes {
		v, err := jsonata.Eval(expr, input)
		if err != nil {
			return nil, fmt.Errorf("cache-set 写入 %s 求值失败: %w", k, err)
		}
		e.cache[k] = v
	}
	return e.cache, nil
}

// gt compares numeric (or string) values for greater-than.
func gt(actual, expected any) (bool, error) {
	af, aok := toFloat(actual)
	ef, eok := toFloat(expected)
	if aok && eok {
		return af > ef, nil
	}
	as, aokS := actual.(string)
	es, eokS := expected.(string)
	if aokS && eokS {
		return as > es, nil
	}
	return false, nil
}

// lt compares numeric (or string) values for less-than.
func lt(actual, expected any) (bool, error) {
	af, aok := toFloat(actual)
	ef, eok := toFloat(expected)
	if aok && eok {
		return af < ef, nil
	}
	as, aokS := actual.(string)
	es, eokS := expected.(string)
	if aokS && eokS {
		return as < es, nil
	}
	return false, nil
}

// toFloat coerces a value to float64 when it is numeric.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// isAuthKey recognizes header authentication keys such as authorization or
// token (mirrors the validator's rule).
func isAuthKey(key string) bool {
	lk := strings.ToLower(key)
	for _, token := range []string{"authorization", "token", "api-key", "apikey", "cookie", "auth"} {
		if strings.Contains(lk, token) {
			return true
		}
	}
	return false
}
