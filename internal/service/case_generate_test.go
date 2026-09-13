package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

// seedCaseFlowWithLeaf creates a case flow with one decomposition node and one
// leaf carrying all detail fields, then saves a version. It returns the case
// flow id and the root node id.
func seedCaseFlowWithLeaf(t *testing.T, db *gorm.DB) (uint, string) {
	t.Helper()
	cf, err := CreateCaseFlow(db, 1, 1, "用例流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	root := view.Tree.Root.ID
	if _, err := AddCaseNode(db, cf.ID, view.Draft.Revision, root, "登录场景"); err != nil {
		t.Fatal(err)
	}
	v2, _ := GetCaseTreeView(db, cf.ID)
	group := v2.Tree.Root.Children[0].ID
	if _, err := AddCaseNode(db, cf.ID, v2.Draft.Revision, group, "正常登录"); err != nil {
		t.Fatal(err)
	}
	v3, _ := GetCaseTreeView(db, cf.ID)
	leaf := v3.Tree.Root.Children[0].Children[0].ID
	pre, in, exp := "已注册账号", "user/pass", "200 + token"
	if _, err := UpdateCaseNode(db, cf.ID, v3.Draft.Revision, leaf, CaseNodeUpdate{Precondition: &pre, Input: &in, Expected: &exp}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCaseFlowVersion(db, cf.ID, 1); err != nil {
		t.Fatal(err)
	}
	return cf.ID, root
}

// TestGenerateFlowFromCasesInjectsCaseContext covers tasks 3.2 and 3.5: the
// generation prompt carries the case hierarchy, every field, the anchor list
// and the self-contained requirement; the LLM only fills the anchors.
func TestGenerateFlowFromCasesInjectsCaseContext(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db)

	f, binding, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{model: "m"}
	res, err := GenerateFlowFromCases(context.Background(), db, f.ID, 1, "", fake)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(fake.visited) == 0 {
		t.Fatal("provider was never called")
	}
	var prompt strings.Builder
	for _, m := range fake.visited[0].Messages {
		if m.Content != nil {
			prompt.WriteString(*m.Content)
			prompt.WriteString("\n")
		}
	}
	text := prompt.String()
	for _, want := range []string{
		"正常登录", "登录场景", "前置条件：已注册账号", "输入：user/pass", "预期结果：200 + token",
		"[锚点 cu", "自包含", "case-unit",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generation context missing %q\n---\n%s", want, text)
		}
	}

	// The pre-built skeleton survives generation; the LLM did not rebuild it.
	draft, _ := GetDraft(db, f.ID)
	tree, _ := flow.ParseTree(draft.Tree)
	if len(tree.CaseUnitNodes()) != len(binding.Leaves) {
		t.Fatalf("case-unit nodes = %d, want %d", len(tree.CaseUnitNodes()), len(binding.Leaves))
	}
	// The unimplemented case is reported (task 3.3).
	if !strings.Contains(res.Message, "尚未实现") {
		t.Fatalf("expected unimplemented-case report, got %q", res.Message)
	}
}

// TestGenerateFlowFromCasesRequiresBinding ensures the generation entry does
// not silently run on a blank flow.
func TestGenerateFlowFromCasesRequiresBinding(t *testing.T) {
	db := setupCaseFlowDB(t)
	f, err := CreateFlow(db, 1, 1, "空白流", "")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{model: "m"}
	if _, err := GenerateFlowFromCases(context.Background(), db, f.ID, 1, "", fake); err == nil {
		t.Fatal("expected error for a flow without a case binding")
	}
}

// TestCrossBranchCacheRejected covers task 3.5: a case branch must not read a
// cache key written by another branch.
func TestCrossBranchCacheRejected(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db)
	f, binding, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := PrebuildCaseUnits(db, f.ID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != 1 {
		t.Fatalf("anchors = %d, want 1", len(anchors))
	}
	draft, _ := GetDraft(db, f.ID)
	tree, _ := flow.ParseTree(draft.Tree)
	// Add a cache-set inside the single case-unit and a sibling reader that
	// references a cache key written by a *different* branch.
	start := tree.Nodes[tree.Start]
	if start == nil {
		t.Fatal("no start")
	}
	// Two case-units: move the reader into a second anchor.
	second := flow.NewNode("cu-second", flow.NodeCaseUnit)
	second.Config = []byte(`{"case_flow_id":1,"case_version_no":1,"case_node_id":"other","title":"另一个用例"}`)
	tree.Nodes["cu-second"] = second
	tree.AddChild(tree.Start, "cu-second")

	writer := flow.NewNode("cs-a", flow.NodeCacheSet)
	writer.Config = []byte(`{"writes":{"token":"$.token"}}`)
	tree.Nodes["cs-a"] = writer
	tree.AddChild(anchors[0].AnchorID, "cs-a")

	reader := flow.NewNode("api-b", flow.NodeAPI)
	reader.Inputs = map[string]flow.IOKey{"auth": {Type: flow.IOTypePrimitive, Source: "$cache.token"}}
	tree.Nodes["api-b"] = reader
	tree.AddChild("cu-second", "api-b")

	res := flow.Validate(tree, flow.ValidatorOptions{})
	found := false
	for _, e := range res.Errors {
		if e.Code == "contract.cache_source_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cross-branch cache reference was not rejected: %+v", res.Errors)
	}
}

// TestCaseExecutionLinkEndToEnd ties the whole closed loop together: bind a
// case flow version, pre-build and fill the skeleton, freeze on save, backfill
// run results, and delete without disturbing case state.
func TestCaseExecutionLinkEndToEnd(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db)

	f, binding, err := CreateFlowFromCases(db, 1, 1, "端到端执行流", "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{model: "m"}
	if _, err := GenerateFlowFromCases(context.Background(), db, f.ID, 1, "", fake); err != nil {
		t.Fatal(err)
	}

	d, err := GetDraft(db, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		t.Fatal(err)
	}
	for i, cu := range tree.CaseUnitNodes() {
		a := flow.NewNode(fmt.Sprintf("a%d", i), flow.NodeAdapter)
		a.Config = []byte(`{"expr":"$"}`)
		tree.Nodes[a.ID] = a
		tree.AddChild(cu.ID, a.ID)
	}
	if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err != nil {
		t.Fatal(err)
	}
	nodeID := binding.Leaves[0].CaseNodeID
	flows, err := ListCaseNodeFlows(db, caseFlowID, nodeID)
	if err != nil || len(flows) != 1 {
		t.Fatalf("node flows = %v (err %v), want 1", flows, err)
	}
	if _, err := TrialRun(db, f.ID, 0); err != nil {
		t.Fatal(err)
	}
	node, err := GetCaseNode(db, caseFlowID, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != caseflow.StatusCovered || node.LastRunResult != model.CaseRunPassed {
		t.Fatalf("node = %+v, want covered+passed", node)
	}
	view, err := FlowCaseSources(db, f.ID)
	if err != nil || !view.Bound {
		t.Fatalf("flow sources = %+v (err %v)", view, err)
	}

	if err := DeleteFlow(db, nil, f.ID); err != nil {
		t.Fatal(err)
	}
	var covCount int64
	db.Model(&model.CaseCoverage{}).Count(&covCount)
	if covCount != 0 {
		t.Fatalf("coverage after delete = %d, want 0", covCount)
	}
	node, err = GetCaseNode(db, caseFlowID, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != caseflow.StatusCovered {
		t.Fatalf("status after delete = %s, want covered", node.Status)
	}
	if node.LastRunResult != model.CaseRunPassed {
		t.Fatalf("run result after delete = %s, want passed", node.LastRunResult)
	}
}
