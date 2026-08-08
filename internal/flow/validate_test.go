package flow

import (
	"bytes"
	"encoding/json"
	"testing"
)

func helperTree(t *testing.T, setup func(t *Tree)) *Tree {
	t.Helper()
	tree := &Tree{Nodes: map[string]*Node{}}
	setup(tree)
	return tree
}

func add(t *Tree, id string, typ NodeType) *Node {
	n := NewNode(id, typ)
	t.Nodes[id] = n
	return n
}

func link(t *Tree, parent, child string) {
	t.AddChild(parent, child)
}

func TestTreeShape(t *testing.T) {
	t.Run("valid single chain", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			a := add(tree, "a", NodeAPI)
			_ = a
			link(tree, "s", "a")
		})
		errs := tree.ValidateTreeShape()
		if len(errs) != 0 {
			t.Fatalf("expected valid tree, got %v", errs)
		}
	})

	t.Run("missing start", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
		})
		if errs := tree.ValidateTreeShape(); len(errs) == 0 {
			t.Fatal("expected missing-start error")
		}
	})

	t.Run("cycle rejected", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			a := add(tree, "a", NodeAPI)
			// parent-pointer cycle: s -> a -> s
			a.Parent = "s"
			tree.Nodes["s"].Parent = "a"
		})
		found := false
		for _, e := range tree.ValidateTreeShape() {
			if e.Code == "tree.cycle" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected cycle error")
		}
	})

	t.Run("fork with two children valid", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "b", NodeAdapter)
			add(tree, "c", NodeAdapter)
			link(tree, "s", "b")
			link(tree, "s", "c")
		})
		if errs := tree.ValidateTreeShape(); len(errs) != 0 {
			t.Fatalf("expected valid fork, got %v", errs)
		}
	})

	t.Run("cache-set participates in tree links", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			cs := add(tree, "cs", NodeCacheSet)
			cs.Parent = "s"
		})
		found := false
		for _, e := range tree.ValidateTreeShape() {
			if e.Code == "cache.linked" {
				found = true
			}
		}
		if found {
			t.Fatal("cache-set 现在可以参与树连线,不应报 cache.linked 错误")
		}
	})

	t.Run("disconnected node rejected", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "orphan", NodeAdapter)
		})
		found := false
		for _, e := range tree.ValidateTreeShape() {
			if e.Code == "tree.disconnected" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected disconnected error")
		}
	})
}

func TestIOContract(t *testing.T) {
	t.Run("unresolved input", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			a := add(tree, "a", NodeAPI)
			a.Inputs["user_id"] = IOKey{Type: IOTypePrimitive}
			link(tree, "s", "a")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "contract.input_unresolved") {
			t.Fatalf("expected input_unresolved, got %v", res.Errors)
		}
	})

	t.Run("satisfied by ancestor output", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			s := tree.Nodes["s"]
			s.Outputs["user_id"] = IOKey{Type: IOTypePrimitive}
			a := add(tree, "a", NodeAPI)
			a.Inputs["user_id"] = IOKey{Type: IOTypePrimitive}
			link(tree, "s", "a")
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid, got %v", res.Errors)
		}
	})
}

func TestAuthHeader(t *testing.T) {
	t.Run("auth key missing source fails", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			a := add(tree, "a", NodeAPI)
			a.Inputs["authorization"] = IOKey{Type: IOTypePrimitive}
			link(tree, "s", "a")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "contract.auth_source_missing") {
			t.Fatalf("expected auth_source_missing, got %v", res.Errors)
		}
	})

	t.Run("auth key satisfied via cache-set, reader not under it", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			a := add(tree, "a", NodeAPI)
			a.Inputs["authorization"] = IOKey{Type: IOTypePrimitive, Source: "$cache.authorization"}
			cs := add(tree, "cs", NodeCacheSet)
			cs.Config = mustConfig(map[string]any{"writes": map[string]string{"authorization": "$.token"}})
			link(tree, "s", "cs")
			link(tree, "s", "a") // reader 不在 cache-set 下方，并列即可
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid, got %v", res.Errors)
		}
	})
}

func TestAdapter(t *testing.T) {
	t.Run("invalid jsonata", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			ad := add(tree, "ad", NodeAdapter)
			ad.Config = mustConfig(map[string]any{"expr": "$$ bad ["})
			link(tree, "s", "ad")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "adapter.jsonata_invalid") {
			t.Fatalf("expected jsonata_invalid, got %v", res.Errors)
		}
	})
}

func TestTryCatch(t *testing.T) {
	t.Run("catch outside try rejected", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "ct", NodeCatch)
			link(tree, "s", "ct")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "trycatch.outside_try") {
			t.Fatalf("expected outside_try, got %v", res.Errors)
		}
	})

	t.Run("nested try accepted", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "tr", NodeTry)
			link(tree, "s", "tr")
			add(tree, "tr2", NodeTry)
			link(tree, "tr", "tr2")
			api := add(tree, "api", NodeAPI)
			api.Inputs["authorization"] = IOKey{Type: IOTypePrimitive}
			cs := add(tree, "cs", NodeCacheSet)
			cs.Config = mustConfig(map[string]any{"writes": map[string]string{"authorization": "$.token"}})
			link(tree, "tr2", "cs")
			link(tree, "cs", "api")
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid nested try, got %v", res.Errors)
		}
	})

	t.Run("branch-local catch passes", func(t *testing.T) {
		// try -> api -> catch: the catch is attached under the api branch's
		// subtree, so it belongs to that specific branch (not a try-level
		// sibling), which is valid.
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "tr", NodeTry)
			link(tree, "s", "tr")
			add(tree, "api", NodeAPI)
			link(tree, "tr", "api")
			add(tree, "ct", NodeCatch)
			link(tree, "api", "ct")
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid branch-local catch, got %v", res.Errors)
		}
	})

	t.Run("sibling catch after executable branch passes", func(t *testing.T) {
		// try -> api, try -> catch: the catch sibling follows an executable
		// branch, so it can consume a pending failure.
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "tr", NodeTry)
			link(tree, "s", "tr")
			add(tree, "api", NodeAPI)
			link(tree, "tr", "api")
			add(tree, "ct", NodeCatch)
			link(tree, "tr", "ct")
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid sibling catch, got %v", res.Errors)
		}
	})

	t.Run("catch claiming unreachable scope rejected", func(t *testing.T) {
		// try -> catch (first child), try -> api: the sibling catch precedes
		// every executable branch, so it claims a scope it can never serve.
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			add(tree, "tr", NodeTry)
			link(tree, "s", "tr")
			add(tree, "ct", NodeCatch)
			link(tree, "tr", "ct")
			add(tree, "api", NodeAPI)
			link(tree, "tr", "api")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "trycatch.catch_unreachable") {
			t.Fatalf("expected catch_unreachable, got %v", res.Errors)
		}
	})
}

func TestLoop(t *testing.T) {
	t.Run("loop input not array", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			lp := add(tree, "lp", NodeLoop)
			lp.Inputs["items"] = IOKey{Type: IOTypePrimitive}
			lp.Config = mustConfig(map[string]any{"input": "items", "var": "it"})
			link(tree, "s", "lp")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "loop.input_not_array") {
			t.Fatalf("expected input_not_array, got %v", res.Errors)
		}
	})

	t.Run("loop with array input valid", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			s := tree.Nodes["s"]
			s.Outputs["items"] = IOKey{Type: IOTypeArray}
			lp := add(tree, "lp", NodeLoop)
			lp.Inputs["items"] = IOKey{Type: IOTypeArray}
			lp.Config = mustConfig(map[string]any{"input": "items", "var": "it"})
			link(tree, "s", "lp")
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid loop, got %v", res.Errors)
		}
	})
}

func TestCacheStaticVisibility(t *testing.T) {
	t.Run("cache key without any writer", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			api := add(tree, "api", NodeAPI)
			api.Inputs["authorization"] = IOKey{Type: IOTypePrimitive, Source: "$cache.token"}
			link(tree, "s", "api")
			// no cache-set node writes "token"
			cs := add(tree, "cs", NodeCacheSet)
			cs.Config = mustConfig(map[string]any{"writes": map[string]string{"other": "$.x"}})
			link(tree, "s", "cs")
		})
		res := Validate(tree, ValidatorOptions{})
		if !hasCode(res, "contract.cache_source_missing") {
			t.Fatalf("expected cache_source_missing, got %v", res.Errors)
		}
	})

	t.Run("cache key with writer valid", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			cs := add(tree, "cs", NodeCacheSet)
			cs.Config = mustConfig(map[string]any{"writes": map[string]string{"token": "$.token"}})
			api := add(tree, "api", NodeAPI)
			api.Inputs["authorization"] = IOKey{Type: IOTypePrimitive, Source: "$cache.token"}
			link(tree, "s", "cs")
			link(tree, "s", "api") // reader 不必在 cache-set 下方
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("expected valid cache reference, got %v", res.Errors)
		}
	})

	t.Run("bare-key cache reference validates", func(t *testing.T) {
		tree := helperTree(t, func(tree *Tree) {
			tree.Start = "s"
			add(tree, "s", NodeStart)
			cs := add(tree, "cs", NodeCacheSet)
			cs.Config = mustConfig(map[string]any{"writes": map[string]string{"token": "$.token"}})
			api := add(tree, "api", NodeAPI)
			api.Inputs["token"] = IOKey{Type: IOTypePrimitive}
			link(tree, "s", "cs")
			link(tree, "s", "api") // bare-key reader 也不必在 cache-set 下方
		})
		res := Validate(tree, ValidatorOptions{})
		if res.HasErrors() {
			t.Fatalf("bare-key cache reference should validate, got %v", res.Errors)
		}
	})
}

func TestSoftDeletedUnit(t *testing.T) {
	tree := helperTree(t, func(tree *Tree) {
		tree.Start = "s"
		add(tree, "s", NodeStart)
		api := add(tree, "api", NodeAPI)
		api.Config = mustConfig(map[string]any{"unit_id": 42})
		link(tree, "s", "api")
	})
	res := Validate(tree, ValidatorOptions{UnitDeleted: func(uid uint) bool { return uid == 42 }})
	if len(res.Warnings) == 0 {
		t.Fatalf("expected soft-delete warning")
	}
}

func mustConfig(v any) []byte {
	b, err := MarshalConfig(v)
	if err != nil {
		panic(err)
	}
	return b
}

func hasCode(res Result, code string) bool {
	for _, e := range res.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// TestResultEmptyArrays guards against nil slices leaking as JSON null: a
// clean tree must serialize errors/warnings as [] rather than null so the
// frontend never hits a null .length access.
func TestResultEmptyArrays(t *testing.T) {
	tree := helperTree(t, func(tree *Tree) {
		tree.Start = "s"
		add(tree, "s", NodeStart)
		a := add(tree, "a", NodeAPI)
		_ = a
		link(tree, "s", "a")
	})
	res := Validate(tree, ValidatorOptions{})
	if res.Errors == nil {
		t.Fatal("expected non-nil Errors slice, got nil")
	}
	if res.Warnings == nil {
		t.Fatal("expected non-nil Warnings slice, got nil")
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !bytes.Contains(b, []byte(`"errors":[]`)) {
		t.Fatalf("expected \"errors\":[] in JSON, got %s", b)
	}
}
