package service

import (
	"fmt"
	"testing"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
)

// TestSaveExecutionFlowFromCasesMarksCoverage covers task 4.1: saving an
// enabled version freezes the case mapping (anchor id + case version) and marks
// the bound case covered, all in one transaction.
func TestSaveExecutionFlowFromCasesMarksCoverage(t *testing.T) {
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
	// Implement the anchor with one execution node so the case counts as done.
	d, err := GetDraft(db, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		t.Fatal(err)
	}
	node := flow.NewNode("a1", flow.NodeAdapter)
	node.Config = []byte(`{"expr":"$"}`)
	tree.Nodes["a1"] = node
	tree.AddChild(anchors[0].AnchorID, "a1")
	if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
		t.Fatal(err)
	}

	version, err := SaveExecutionFlowFromCases(db, f.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if version.VersionNo != 1 {
		t.Fatalf("version no = %d, want 1", version.VersionNo)
	}

	var cn model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, binding.Leaves[0].CaseNodeID).First(&cn).Error; err != nil {
		t.Fatal(err)
	}
	if cn.Status != caseflow.StatusCovered {
		t.Fatalf("status = %s, want covered", cn.Status)
	}
	var cov model.CaseCoverage
	if err := db.Where("case_node_id = ? AND flow_version_id = ?", cn.ID, version.ID).First(&cov).Error; err != nil {
		t.Fatal(err)
	}
	if cov.AnchorNodeID != anchors[0].AnchorID {
		t.Fatalf("anchor = %s, want %s", cov.AnchorNodeID, anchors[0].AnchorID)
	}
	if cov.CaseVersionNo != 1 {
		t.Fatalf("case version = %d, want 1", cov.CaseVersionNo)
	}
}

// TestSaveVersionFailureLeavesNoMapping covers task 4.1's rollback clause: a
// failing save must not create coverage or change case status.
func TestSaveVersionFailureLeavesNoMapping(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db)
	f, binding, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrebuildCaseUnits(db, f.ID, binding); err != nil {
		t.Fatal(err)
	}
	// The skeleton has empty case-units → full validation fails (unimplemented).
	if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err == nil {
		t.Fatal("expected validation failure for unimplemented case")
	}
	var covCount int64
	db.Model(&model.CaseCoverage{}).Count(&covCount)
	if covCount != 0 {
		t.Fatalf("coverage rows = %d, want 0", covCount)
	}
	var cn model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, binding.Leaves[0].CaseNodeID).First(&cn).Error; err != nil {
		t.Fatal(err)
	}
	if cn.Status != caseflow.StatusUncovered {
		t.Fatalf("status = %s, want uncovered", cn.Status)
	}
}

// TestRunBackfillsCaseResults covers task 4.2: trial and version runs both
// backfill the latest branch outcome onto the case node, and run results stay
// independent from the implementation status.
func TestRunBackfillsCaseResults(t *testing.T) {
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
	setAdapter := func(expr string) {
		d, err := GetDraft(db, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := flow.ParseTree(d.Tree)
		if err != nil {
			t.Fatal(err)
		}
		n := flow.NewNode("a1", flow.NodeAdapter)
		n.Config = []byte(fmt.Sprintf(`{"expr":%q}`, expr))
		tree.Nodes["a1"] = n
		tree.AddChild(anchors[0].AnchorID, "a1")
		if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
			t.Fatal(err)
		}
	}

	setAdapter("$")
	if _, err := TrialRun(db, f.ID, 0); err != nil {
		t.Fatal(err)
	}
	var cn model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, binding.Leaves[0].CaseNodeID).First(&cn).Error; err != nil {
		t.Fatal(err)
	}
	if cn.LastRunResult != model.CaseRunPassed {
		t.Fatalf("trial result = %s, want passed", cn.LastRunResult)
	}
	if cn.LastRunAt == nil {
		t.Fatal("last_run_at not set")
	}

	// Saving the version marks covered; a version run also backfills.
	if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := RunVersion(db, f.ID, 1, 0); err != nil {
		t.Fatal(err)
	}
	db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, binding.Leaves[0].CaseNodeID).First(&cn)
	if cn.Status != caseflow.StatusCovered {
		t.Fatalf("status = %s, want covered", cn.Status)
	}
	if cn.LastRunResult != model.CaseRunPassed {
		t.Fatalf("version run result = %s, want passed", cn.LastRunResult)
	}

	// A later failing trial run updates the result but not the status.
	setAdapter("(")
	if _, err := TrialRun(db, f.ID, 0); err != nil {
		t.Fatal(err)
	}
	db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, binding.Leaves[0].CaseNodeID).First(&cn)
	if cn.LastRunResult != model.CaseRunFailed {
		t.Fatalf("result = %s, want failed", cn.LastRunResult)
	}
	if cn.Status != caseflow.StatusCovered {
		t.Fatalf("status changed to %s, want covered", cn.Status)
	}
}

// TestDeleteFlowCleansOnlyItsCoverage covers task 4.3: deleting one execution
// flow removes only its own coverage rows, leaving CaseNode status and other
// flow associations intact.
func TestDeleteFlowCleansOnlyItsCoverage(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db)
	build := func(name string) (uint, string) {
		f, binding, err := CreateFlowFromCases(db, 1, 1, name, "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
		if err != nil {
			t.Fatal(err)
		}
		anchors, err := PrebuildCaseUnits(db, f.ID, binding)
		if err != nil {
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
		n := flow.NewNode("a1", flow.NodeAdapter)
		n.Config = []byte(`{"expr":"$"}`)
		tree.Nodes["a1"] = n
		tree.AddChild(anchors[0].AnchorID, "a1")
		if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err != nil {
			t.Fatal(err)
		}
		return f.ID, binding.Leaves[0].CaseNodeID
	}
	flowA, leafKey := build("A")
	flowB, _ := build("B")

	countFor := func(flowID uint) int64 {
		var n int64
		db.Model(&model.CaseCoverage{}).
			Joins("JOIN flow_versions ON flow_versions.id = case_coverages.flow_version_id").
			Where("flow_versions.flow_id = ?", flowID).Count(&n)
		return n
	}
	if countFor(flowA) != 1 || countFor(flowB) != 1 {
		t.Fatalf("coverage before: A=%d B=%d, want 1/1", countFor(flowA), countFor(flowB))
	}

	if err := DeleteFlow(db, nil, flowA); err != nil {
		t.Fatal(err)
	}
	if countFor(flowA) != 0 {
		t.Fatalf("coverage A after delete = %d, want 0", countFor(flowA))
	}
	if countFor(flowB) != 1 {
		t.Fatalf("coverage B after delete = %d, want 1", countFor(flowB))
	}
	var cn model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, leafKey).First(&cn).Error; err != nil {
		t.Fatal(err)
	}
	if cn.Status != caseflow.StatusCovered {
		t.Fatalf("status after delete = %s, want covered", cn.Status)
	}
}

// TestCaseFlowRestoreKeepsMappingAndProvenance covers task 4.4: restoring a
// case flow version does not delete mappings or auto-create flows, and the
// bound case version stays identifiable after the case flow advances.
func TestCaseFlowRestoreKeepsMappingAndProvenance(t *testing.T) {
	db := setupCaseFlowDB(t)
	caseFlowID, root := seedCaseFlowWithLeaf(t, db) // saves version 1
	f, binding, err := CreateFlowFromCases(db, 1, 1, "执行流", "", []CaseSelection{{CaseFlowID: caseFlowID, RootIDs: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := PrebuildCaseUnits(db, f.ID, binding)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := GetDraft(db, f.ID)
	tree, _ := flow.ParseTree(d.Tree)
	n := flow.NewNode("a1", flow.NodeAdapter)
	n.Config = []byte(`{"expr":"$"}`)
	tree.Nodes["a1"] = n
	tree.AddChild(anchors[0].AnchorID, "a1")
	if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err != nil {
		t.Fatal(err)
	}

	var cov model.CaseCoverage
	if err := db.First(&cov).Error; err != nil {
		t.Fatal(err)
	}
	if cov.CaseVersionNo != 1 {
		t.Fatalf("case version = %d, want 1", cov.CaseVersionNo)
	}

	// Advance the case flow to a new version; the existing mapping keeps v1.
	view, _ := GetCaseTreeView(db, caseFlowID)
	if _, err := AddCaseNode(db, caseFlowID, view.Draft.Revision, root, "第二个用例"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCaseFlowVersion(db, caseFlowID, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&cov).Error; err != nil {
		t.Fatal(err)
	}
	if cov.CaseVersionNo != 1 {
		t.Fatalf("after new case version, bound version = %d, want 1", cov.CaseVersionNo)
	}

	var covBefore, flowBefore int64
	db.Model(&model.CaseCoverage{}).Count(&covBefore)
	db.Model(&model.TestFlow{}).Count(&flowBefore)
	if _, err := RestoreCaseFlowVersion(db, caseFlowID, 1); err != nil {
		t.Fatal(err)
	}
	var covAfter, flowAfter int64
	db.Model(&model.CaseCoverage{}).Count(&covAfter)
	db.Model(&model.TestFlow{}).Count(&flowAfter)
	if covAfter != covBefore {
		t.Fatalf("coverage changed on restore: %d -> %d", covBefore, covAfter)
	}
	if flowAfter != flowBefore {
		t.Fatalf("flows changed on restore: %d -> %d", flowBefore, flowAfter)
	}
}

// TestListCaseNodeFlowsAndFlowSources covers task 5.1/5.2: the case-side view
// returns the implementing flow, version and anchor; the flow-side view returns
// the binding.
func TestListCaseNodeFlowsAndFlowSources(t *testing.T) {
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
	d, _ := GetDraft(db, f.ID)
	tree, _ := flow.ParseTree(d.Tree)
	n := flow.NewNode("a1", flow.NodeAdapter)
	n.Config = []byte(`{"expr":"$"}`)
	tree.Nodes["a1"] = n
	tree.AddChild(anchors[0].AnchorID, "a1")
	if _, err := UpdateDraft(db, f.ID, d.Name, tree.String(), nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveExecutionFlowFromCases(db, f.ID, 1); err != nil {
		t.Fatal(err)
	}

	nodeID := binding.Leaves[0].CaseNodeID
	flows, err := ListCaseNodeFlows(db, caseFlowID, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 {
		t.Fatalf("flows = %d, want 1", len(flows))
	}
	if flows[0].FlowName != "执行流" || flows[0].AnchorNodeID != anchors[0].AnchorID || flows[0].CaseVersionNo != 1 || !flows[0].Enabled {
		t.Fatalf("unexpected flow view: %+v", flows[0])
	}
	node, err := GetCaseNode(db, caseFlowID, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != caseflow.StatusCovered {
		t.Fatalf("node status = %s", node.Status)
	}

	view, err := FlowCaseSources(db, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Bound || view.Binding == nil || len(view.Binding.Leaves) != 1 {
		t.Fatalf("flow sources view = %+v", view)
	}

	// A blank flow reports no binding.
	blank, err := CreateFlow(db, 1, 1, "空白流", "")
	if err != nil {
		t.Fatal(err)
	}
	blankView, err := FlowCaseSources(db, blank.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blankView.Bound {
		t.Fatal("blank flow should not report a binding")
	}
}
