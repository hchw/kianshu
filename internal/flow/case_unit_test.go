package flow

import (
	"encoding/json"
	"testing"
)

func TestCaseUnitBinding(t *testing.T) {
	n := NewNode("c1", NodeCaseUnit)
	n.Config = json.RawMessage(`{"case_flow_id":7,"case_version_no":3,"case_node_id":"c9a1","title":"正常登录","extra":"ignored"}`)

	cfg, ok := CaseUnitBinding(n)
	if !ok {
		t.Fatal("expected a usable binding")
	}
	if cfg.CaseFlowID != 7 || cfg.CaseVersionNo != 3 || cfg.CaseNodeID != "c9a1" || cfg.Title != "正常登录" {
		t.Fatalf("binding = %+v", cfg)
	}

	// Round trip through the canonical marshal helper.
	raw, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back CaseUnitConfig
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != cfg {
		t.Fatalf("round trip = %+v, want %+v", back, cfg)
	}

	// A case-unit without a case identity is not usable.
	if _, ok := CaseUnitBinding(NewNode("c2", NodeCaseUnit)); ok {
		t.Fatal("expected no binding for empty config")
	}
	// Non case-unit nodes never carry a binding.
	other := NewNode("s", NodeStart)
	other.Config = n.Config
	if _, ok := CaseUnitBinding(other); ok {
		t.Fatal("expected no binding for non case-unit node")
	}
}

func TestCaseUnitNodes(t *testing.T) {
	tree := helperTree(t, func(tree *Tree) {
		tree.Start = "s"
		add(tree, "s", NodeStart)
		add(tree, "cu-b", NodeCaseUnit)
		add(tree, "cu-a", NodeCaseUnit)
		add(tree, "a", NodeAPI)
	})
	got := tree.CaseUnitNodes()
	if len(got) != 2 {
		t.Fatalf("count = %d, want 2", len(got))
	}
	if got[0].ID != "cu-a" || got[1].ID != "cu-b" {
		t.Fatalf("order = %s,%s; want sorted cu-a,cu-b", got[0].ID, got[1].ID)
	}
}

func TestCaseUnitValidation(t *testing.T) {
	find := func(errs []ValidationError, code string) bool {
		for _, e := range errs {
			if e.Code == code {
				return true
			}
		}
		return false
	}

	build := func(mutate func(cu *Node)) []ValidationError {
		tree := &Tree{Nodes: map[string]*Node{}}
		tree.Start = "s"
		tree.Nodes["s"] = NewNode("s", NodeStart)
		cu := NewNode("cu1", NodeCaseUnit)
		cu.Config = json.RawMessage(`{"case_flow_id":1,"case_version_no":1,"case_node_id":"case-a","title":"A"}`)
		tree.Nodes["cu1"] = cu
		tree.Nodes["a"] = NewNode("a", NodeAPI)
		tree.AddChild("s", "cu1")
		tree.AddChild("cu1", "a")
		if mutate != nil {
			mutate(cu)
		}
		return tree.validateCaseUnits()
	}

	if errs := build(nil); len(errs) != 0 {
		t.Fatalf("valid case-unit reported errors: %v", errs)
	}
	if errs := build(func(cu *Node) { cu.Config = nil }); !find(errs, "case_unit.binding_missing") {
		t.Fatalf("missing binding not reported: %v", errs)
	}
	if errs := build(func(cu *Node) { cu.Children = nil }); !find(errs, "case_unit.unimplemented") {
		t.Fatalf("unimplemented case-unit not reported: %v", errs)
	}
	if errs := build(func(cu *Node) { cu.Parent = "a" }); !find(errs, "case_unit.parent_invalid") {
		t.Fatalf("non-start parent not reported: %v", errs)
	}
	t.Run("case-unit only validated when present", func(t *testing.T) {
		tree := &Tree{Nodes: map[string]*Node{}}
		tree.Start = "s"
		tree.Nodes["s"] = NewNode("s", NodeStart)
		tree.Nodes["a"] = NewNode("a", NodeAPI)
		tree.AddChild("s", "a")
		if errs := tree.validateCaseUnits(); len(errs) != 0 {
			t.Fatalf("blank flow reported case-unit errors: %v", errs)
		}
	})
}
