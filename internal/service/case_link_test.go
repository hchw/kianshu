package service

import (
	"errors"
	"testing"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
)

// TestCaseLinkSchemaDefaults verifies the new binding/mapping/result fields are
// backward compatible: absent values keep the pre-change behavior, and a case
// may map to several anchors within one execution flow version.
func TestCaseLinkSchemaDefaults(t *testing.T) {
	gdb := testDB(t)

	f := model.TestFlow{TestSetID: 1, Name: "f"}
	if err := gdb.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	d := model.FlowDraft{FlowID: f.ID, Name: "f", Tree: `{"start":"n1","nodes":{"n1":{"id":"n1","type":"start"}}}`}
	if err := gdb.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	var got model.FlowDraft
	if err := gdb.First(&got, d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.CaseBinding != "" {
		t.Fatalf("case_binding = %q, want empty (equivalent to before)", got.CaseBinding)
	}

	n := model.CaseNode{CaseFlowID: 1, NodeKey: "c1"}
	if err := gdb.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	var gn model.CaseNode
	if err := gdb.First(&gn, n.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gn.LastRunResult != model.CaseRunNotRun {
		t.Fatalf("last_run_result = %q, want %q", gn.LastRunResult, model.CaseRunNotRun)
	}
	if gn.Status != "uncovered" {
		t.Fatalf("status = %q, want uncovered", gn.Status)
	}
	if gn.LastRunAt != nil {
		t.Fatalf("last_run_at = %v, want nil", gn.LastRunAt)
	}

	// Two anchors for the same case in the same version must coexist.
	covA := model.CaseCoverage{CaseNodeID: n.ID, FlowVersionID: 9, AnchorNodeID: "cu-a", CaseVersionNo: 3}
	covB := model.CaseCoverage{CaseNodeID: n.ID, FlowVersionID: 9, AnchorNodeID: "cu-b", CaseVersionNo: 3}
	if err := gdb.Create(&covA).Error; err != nil {
		t.Fatalf("first anchor: %v", err)
	}
	if err := gdb.Create(&covB).Error; err != nil {
		t.Fatalf("second anchor rejected: %v", err)
	}
	var count int64
	gdb.Model(&model.CaseCoverage{}).Where("case_node_id = ? AND flow_version_id = ?", n.ID, 9).Count(&count)
	if count != 2 {
		t.Fatalf("coverage count = %d, want 2", count)
	}
}

func TestResolveCaseBindingExpandsLeavesAndRequiresVersion(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "用例流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}

	// Draft-only: no saved version yet → must be rejected.
	if _, err := ResolveCaseBinding(db, []CaseSelection{{CaseFlowID: cf.ID, RootIDs: []string{"x"}}}); !errors.Is(err, ErrCaseFlowVersionRequired) {
		t.Fatalf("want ErrCaseFlowVersionRequired, got %v", err)
	}

	view, _ := GetCaseTreeView(db, cf.ID)
	root := view.Tree.Root.ID
	if _, err := AddCaseNode(db, cf.ID, view.Draft.Revision, root, "正常登录"); err != nil {
		t.Fatal(err)
	}
	view2, _ := GetCaseTreeView(db, cf.ID)
	childA := view2.Tree.Root.Children[0].ID
	if _, err := AddCaseNode(db, cf.ID, view2.Draft.Revision, root, "异常密码"); err != nil {
		t.Fatal(err)
	}
	view3, _ := GetCaseTreeView(db, cf.ID)
	if len(view3.Tree.Root.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(view3.Tree.Root.Children))
	}
	childB := view3.Tree.Root.Children[1].ID

	v, err := SaveCaseFlowVersion(db, cf.ID, 1)
	if err != nil {
		t.Fatal(err)
	}

	b, err := ResolveCaseBinding(db, []CaseSelection{{CaseFlowID: cf.ID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sources) != 1 || b.Sources[0].VersionNo != v.VersionNo || b.Sources[0].Name != "用例流" {
		t.Fatalf("sources = %+v", b.Sources)
	}
	if len(b.Leaves) != 2 {
		t.Fatalf("leaves = %d, want 2", len(b.Leaves))
	}
	set := map[string]string{}
	for _, l := range b.Leaves {
		set[l.CaseNodeID] = l.Title
	}
	if set[childA] != "正常登录" || set[childB] != "异常密码" {
		t.Fatalf("leaves = %+v", set)
	}
	if _, ok := b.Resolved[cf.ID]; !ok {
		t.Fatalf("resolved sources missing for case flow %d", cf.ID)
	}

	// Binding must survive a plain draft save.
	f, err := CreateFlow(db, 1, 1, "执行流", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveFlowCaseBinding(db, f.ID, b); err != nil {
		t.Fatal(err)
	}
	draft, err := GetDraft(db, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateDraft(db, f.ID, draft.Name, draft.Tree, nil, ""); err != nil {
		t.Fatal(err)
	}
	got, ok, err := GetFlowCaseBinding(db, f.ID)
	if err != nil || !ok {
		t.Fatalf("binding lost: ok=%v err=%v", ok, err)
	}
	if len(got.Leaves) != 2 || len(got.Sources) != 1 {
		t.Fatalf("binding after save = %+v", got)
	}
}

func TestCreateFlowFromCasesRejectsDraftOnly(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "无版本用例流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	if _, _, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: cf.ID, RootIDs: []string{view.Tree.Root.ID}}}); !errors.Is(err, ErrCaseFlowVersionRequired) {
		t.Fatalf("want ErrCaseFlowVersionRequired, got %v", err)
	}
	// No flow must have been created.
	var count int64
	db.Model(&model.TestFlow{}).Count(&count)
	if count != 0 {
		t.Fatalf("flows = %d, want 0", count)
	}
}

// TestPrebuildCaseUnitsCreatesAnchorsInOrder covers task 3.1: one case-unit
// anchor per selected case leaf, flat under start, in case-tree order, and
// decomposition nodes do not become nodes.
func TestPrebuildCaseUnitsCreatesAnchorsInOrder(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "用例流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	root := view.Tree.Root.ID
	// Decomposition node "登录场景" with two leaves.
	if _, err := AddCaseNode(db, cf.ID, view.Draft.Revision, root, "登录场景"); err != nil {
		t.Fatal(err)
	}
	v2, _ := GetCaseTreeView(db, cf.ID)
	group := v2.Tree.Root.Children[0].ID
	if _, err := AddCaseNode(db, cf.ID, v2.Draft.Revision, group, "正常登录"); err != nil {
		t.Fatal(err)
	}
	v3, _ := GetCaseTreeView(db, cf.ID)
	if _, err := AddCaseNode(db, cf.ID, v3.Draft.Revision, group, "异常密码"); err != nil {
		t.Fatal(err)
	}
	v4, _ := GetCaseTreeView(db, cf.ID)
	if _, err := AddCaseNode(db, cf.ID, v4.Draft.Revision, root, "下单"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCaseFlowVersion(db, cf.ID, 1); err != nil {
		t.Fatal(err)
	}

	f, binding, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: cf.ID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(binding.Leaves) != 3 {
		t.Fatalf("leaves = %d, want 3", len(binding.Leaves))
	}

	anchors, err := PrebuildCaseUnits(db, f.ID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != 3 {
		t.Fatalf("anchors = %d, want 3", len(anchors))
	}

	draft, _ := GetDraft(db, f.ID)
	tree, err := flow.ParseTree(draft.Tree)
	if err != nil {
		t.Fatal(err)
	}
	cus := tree.CaseUnitNodes()
	if len(cus) != 3 {
		t.Fatalf("case-unit nodes = %d, want 3", len(cus))
	}
	// All anchors hang directly under start, in leaf order.
	if len(tree.Nodes[tree.Start].Children) != 3 {
		t.Fatalf("start children = %v, want 3 anchors", tree.Nodes[tree.Start].Children)
	}
	for i, a := range anchors {
		if tree.Nodes[tree.Start].Children[i] != a.AnchorID {
			t.Fatalf("anchor %d order = %s, want %s", i, tree.Nodes[tree.Start].Children[i], a.AnchorID)
		}
		cfg, ok := flow.CaseUnitBinding(tree.Nodes[a.AnchorID])
		if !ok {
			t.Fatalf("anchor %s has no binding", a.AnchorID)
		}
		if cfg.CaseNodeID != binding.Leaves[i].CaseNodeID {
			t.Fatalf("anchor %d case = %s, want %s", i, cfg.CaseNodeID, binding.Leaves[i].CaseNodeID)
		}
	}

	// Every case is unimplemented until the branches are filled.
	if missing := MissingCaseUnits(tree, binding); len(missing) != 3 {
		t.Fatalf("missing = %d, want 3", len(missing))
	}
	// Re-generating rebuilds the same count without duplicating anchors.
	if _, err := PrebuildCaseUnits(db, f.ID, binding); err != nil {
		t.Fatal(err)
	}
	draft2, _ := GetDraft(db, f.ID)
	tree2, _ := flow.ParseTree(draft2.Tree)
	if len(tree2.CaseUnitNodes()) != 3 {
		t.Fatalf("after rebuild case-unit nodes = %d, want 3", len(tree2.CaseUnitNodes()))
	}
}
