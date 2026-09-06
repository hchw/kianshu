package service

import (
	"testing"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"
)

func TestSaveExecutionFlowFromCasesMarksCoverage(t *testing.T) {
	db := setupCaseFlowDB(t)
	flow, err := CreateFlow(db, 1, 1, "执行流", "")
	if err != nil {
		t.Fatal(err)
	}
	cf, err := CreateCaseFlow(db, 1, 1, "用例流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	rootID := view.Tree.Root.ID

	version, err := SaveExecutionFlowFromCases(db, flow.ID, 1, []CaseSelection{{CaseFlowID: cf.ID, RootIDs: []string{rootID}}})
	if err != nil {
		t.Fatal(err)
	}
	if version.VersionNo != 1 {
		t.Fatalf("version no = %d", version.VersionNo)
	}

	var cn model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", cf.ID, rootID).First(&cn).Error; err != nil {
		t.Fatal(err)
	}
	if cn.Status != caseflow.StatusCovered {
		t.Fatalf("status = %s", cn.Status)
	}
	var count int64
	db.Model(&model.CaseCoverage{}).Where("case_node_id = ? AND flow_version_id = ?", cn.ID, version.ID).Count(&count)
	if count != 1 {
		t.Fatalf("coverage count = %d", count)
	}
}
