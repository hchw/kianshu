package exec

import (
	"fmt"
	"time"

	"github/hchw/kianshu/internal/flow"
)

// tryScope is the runtime state of one try node's protected scope.
type tryScope struct {
	// pendingFailure records a failure in this scope that no branch-local
	// catch has handled yet; the next catch sibling branch will consume it.
	pendingFailure bool
	// softStopped records that a nested try/loop soft-stopped inside this
	// scope: the try soft-stops too, but the soft-stop is never routed to a
	// catch (containment, no bubbling).
	softStopped bool
	// output is the value the try passes through when every branch succeeds:
	// the output of the last successful branch (catch pass-through).
	output any
}

// runTry executes a try node's protected scope with the containment
// semantics defined in the design (option B, no bubbling).
func (e *engine) runTry(n *flow.Node, parentOut any) Status {
	started := time.Now()
	scope := &tryScope{}
	// Walk the try's children in order. Each child is a branch; a catch
	// child consumes a pending failure as a fallback, otherwise passes the
	// preceding output through.
	for _, child := range n.Children {
		cn, ok := e.tree.Nodes[child]
		if !ok || cn == nil {
			continue
		}
		if cn.Type == flow.NodeCatch {
			scope.execCatch(e, child, cn, parentOut)
			continue
		}
		// A regular branch: run it, then decide how its outcome is contained.
		st := e.runNode(child, parentOut)
		switch st {
		case StatusFailed:
			// Look for a branch-local catch inside the failing subtree; if
			// found it handles the failure with its fallback. Otherwise the
			// failure stays pending for a later catch sibling branch.
			if catchID := e.findCatchInSubtree(child, n.ID); catchID != "" {
				scope.output = e.execCatchFailure(catchID, n.ID, parentOut)
			} else {
				scope.pendingFailure = true
			}
		case StatusOK:
			if r, ok := e.results[child]; ok {
				scope.output = r.Output
			}
		default:
			// A soft-stopped branch (nested try without catch, failed loop)
			// is contained: it must not bubble to an outer catch, but the
			// enclosing try soft-stops as well.
			scope.softStopped = true
		}
	}
	status := StatusOK
	if scope.pendingFailure || scope.softStopped {
		status = StatusSoftStop
	}
	e.record(n.ID, status, parentOut, scope.output, nil)
	e.results[n.ID].StartedAt = started
	return status
}

// execCatch runs a catch node reached as a sibling branch. With a pending
// failure it emits the configured fallback and consumes the failure;
// otherwise it passes the preceding output through.
func (s *tryScope) execCatch(e *engine, id string, n *flow.Node, parentOut any) {
	if s.pendingFailure {
		s.output = e.execCatchFailure(id, "", parentOut)
		s.pendingFailure = false
		return
	}
	// Pass-through: re-run the catch node normally so its result records the
	// same value the preceding output carried.
	st := e.runNode(id, parentOut)
	if st == StatusOK {
		if r, ok := e.results[id]; ok {
			s.output = r.Output
		}
	}
}

// execCatchFailure executes a catch node in failure mode: it outputs the
// configured fallback value and then walks the catch's own children. The
// failure is contained within this branch. When the catch already ran as part
// of the failing branch's own walk (a sibling failed after it executed), only
// its recorded output is replaced with the fallback so already-executed
// side effects never run a second time.
func (e *engine) execCatchFailure(id, tryID string, parentOut any) any {
	n, ok := e.tree.Nodes[id]
	if !ok || n == nil {
		return nil
	}
	fallback := e.catchFallback(n)
	if _, ran := e.results[id]; ran {
		e.results[id].Output = fallback
		e.results[id].Status = StatusOK
		e.doneCatch[id] = true
		return fallback
	}
	e.doneCatch[id] = true
	e.record(id, StatusOK, parentOut, fallback, nil)
	// Continue with the catch's children, carrying the fallback as output.
	for _, child := range n.Children {
		e.runNode(child, fallback)
	}
	return fallback
}

// catchFallback reads a catch node's configured fallback value.
func (e *engine) catchFallback(n *flow.Node) any {
	var cfg struct {
		Fallback any `json:"fallback"`
	}
	_ = flow.UnmarshalConfig(n, &cfg)
	return cfg.Fallback
}

// findCatchInSubtree returns the first catch node inside the subtree rooted
// at rootID that belongs to tryID's scope (not to a nested try). Empty when
// none exists.
func (e *engine) findCatchInSubtree(rootID, tryID string) string {
	stack := []string{rootID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		cn, ok := e.tree.Nodes[id]
		if !ok || cn == nil {
			continue
		}
		if cn.Type == flow.NodeCatch && e.nearestTry(id) == tryID {
			return id
		}
		if cn.Type == flow.NodeTry && id != tryID {
			// Skip nested try subtrees: their catches belong to them.
			continue
		}
		for _, c := range cn.Children {
			stack = append(stack, c)
		}
	}
	return ""
}

// nearestTry returns the id of the nearest enclosing try node of id (the
// closest try ancestor), or empty.
func (e *engine) nearestTry(id string) string {
	for _, a := range e.tree.Ancestors(id) {
		if n, ok := e.tree.Nodes[a]; ok && n != nil && n.Type == flow.NodeTry {
			return a
		}
	}
	return ""
}

// runLoop iterates the node's subtree over each element of its array input,
// collecting per-iteration results into an array. Any failed iteration
// soft-stops the loop without an aggregated output.
func (e *engine) runLoop(n *flow.Node, parentOut any) Status {
	started := time.Now()
	if err := e.contextErr(); err != nil {
		e.record(n.ID, StatusFailed, nil, nil, err)
		return StatusFailed
	}
	input := asInputMap(e.resolveInputs(n, parentOut))
	var cfg struct {
		Input string `json:"input"`
		Var   string `json:"var"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		e.record(n.ID, StatusFailed, input, nil, err)
		return StatusFailed
	}
	if cfg.Input == "" || cfg.Var == "" {
		err := fmt.Errorf("loop 需要 input 与 var 字段")
		e.record(n.ID, StatusFailed, input, nil, err)
		return StatusFailed
	}
	arr, ok := input[cfg.Input].([]any)
	if !ok {
		// Also accept a typed slice.
		arr = toSlice(input[cfg.Input])
		if arr == nil {
			err := fmt.Errorf("loop 输入 %s 不是数组", cfg.Input)
			e.record(n.ID, StatusFailed, input, nil, err)
			return StatusFailed
		}
	}
	results := []any{}
	status := StatusOK
	// Save/restore the iteration context so nested loops see their own var.
	prevCtx := e.iterCtx
	defer func() { e.iterCtx = prevCtx }()
	for _, item := range arr {
		e.iterCtx = map[string]any{cfg.Var: item}
		iterStatus := StatusOK
		// Collect every successful child's output for the iteration: a single
		// child yields its output directly, several children aggregate into an
		// object keyed by child id.
		childrenOut := map[string]any{}
		var single any
		for _, child := range n.Children {
			if st := e.runNode(child, e.iterCtx); st == StatusFailed {
				iterStatus = StatusFailed
				break
			} else if st == StatusOK {
				if r, ok := e.results[child]; ok {
					childrenOut[child] = r.Output
					single = r.Output
				}
			}
		}
		if iterStatus != StatusOK {
			status = StatusSoftStop
			break
		}
		var iterOut any
		if len(childrenOut) == 1 {
			iterOut = single
		} else {
			iterOut = childrenOut
		}
		results = append(results, iterOut)
	}
	var out any = results
	if status == StatusSoftStop {
		out = nil
	}
	e.record(n.ID, status, input, out, nil)
	e.results[n.ID].StartedAt = started
	return status
}

// toSlice coerces a value into a []any when it is a typed slice.
func toSlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, 0, len(t))
		for _, s := range t {
			out = append(out, s)
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(t))
		for _, m := range t {
			out = append(out, m)
		}
		return out
	default:
		return nil
	}
}

// resolveInputs resolves every declared input key to a value per the
// contract: explicit $cache.source, an ancestor output key, or a bare key
// name found in an ancestor's output, the shared cache, or the current loop
// iteration context. A node that declares no input keys consumes its
// parent's output directly (chain semantics). A cache key that no cache-set
// has written yet triggers that cache-set lazily with the current parent
// output as its input, so the "login api → cache-set → downstream $cache
// read" chain works even though cache-sets participate in no tree links.
func (e *engine) resolveInputs(n *flow.Node, parentOut any) any {
	if len(n.Inputs) == 0 {
		return parentOut
	}
	resolved := map[string]any{}
	// Collect ancestor outputs (by ancestor id) for key lookup.
	ancOutputs := map[string]any{}
	for _, a := range e.tree.Ancestors(n.ID) {
		if r, ok := e.results[a]; ok && r.Output != nil {
			ancOutputs[a] = r.Output
		}
	}
	// Flatten ancestor outputs that are flat maps into the resolution scope.
	ancestorScope := map[string]any{}
	for _, a := range e.tree.Ancestors(n.ID) {
		r, ok := e.results[a]
		if !ok || r.Output == nil {
			continue
		}
		if m, ok := r.Output.(map[string]any); ok {
			for k, v := range m {
				if _, exists := ancestorScope[k]; !exists {
					ancestorScope[k] = v
				}
			}
		}
	}
	// ensureCache runs the first cache-set that writes key and has not run
	// yet, with the current parent output as its input.
	ensureCache := func(key string) {
		if _, ok := e.cache[key]; ok {
			return
		}
		for _, csID := range e.tree.CacheSets {
			if e.cacheSetRan[csID] {
				continue
			}
			cs, ok := e.tree.Nodes[csID]
			if !ok || cs == nil || cs.Type != flow.NodeCacheSet {
				continue
			}
			if !e.cacheSetWrites(cs, key) {
				continue
			}
			e.cacheSetRan[csID] = true
			in := asInputMap(parentOut)
			out, err := e.execute(cs, in)
			if err != nil {
				e.record(csID, StatusFailed, in, nil, err)
				continue
			}
			e.record(csID, StatusOK, in, out, nil)
		}
	}
	for key, io := range n.Inputs {
		switch {
		case io.Source != "":
			if ck, ok := flow.CacheKey(io.Source); ok {
				ensureCache(ck)
				if v, ok := e.cache[ck]; ok {
					resolved[key] = v
				}
				continue
			}
			if v, ok := ancestorScope[io.Source]; ok {
				resolved[key] = v
			}
		default:
			if v, ok := ancestorScope[key]; ok {
				resolved[key] = v
			} else if v, ok := e.cache[key]; ok {
				resolved[key] = v
			} else if v, ok := e.iterCtx[key]; ok {
				resolved[key] = v
			} else {
				// A bare key naming a cache-set write key (no $cache. prefix)
				// still triggers its lazy execution, matching validation.
				ensureCache(key)
				if v, ok := e.cache[key]; ok {
					resolved[key] = v
				}
			}
		}
	}
	// Bare cache keys are also resolvable for input keys that only name them.
	for k, v := range e.cache {
		if _, exists := resolved[k]; !exists {
			if _, declared := n.Inputs[k]; declared {
				resolved[k] = v
			}
		}
	}
	return resolved
}

// cacheSetWrites reports whether the cache-set's config writes the given key.
func (e *engine) cacheSetWrites(n *flow.Node, key string) bool {
	var cfg struct {
		Writes map[string]string `json:"writes"`
	}
	if err := flow.UnmarshalConfig(n, &cfg); err != nil {
		return false
	}
	_, ok := cfg.Writes[key]
	return ok
}
