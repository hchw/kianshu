package service

import (
	"testing"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupCaseFlowDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	u := &model.User{Username: "u", PasswordHash: "x"}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	ts := &model.TestSet{Name: "ts", OwnerID: u.ID}
	if err := db.Create(ts).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateCaseFlowRequiresSource(t *testing.T) {
	db := setupCaseFlowDB(t)
	if _, err := CreateCaseFlow(db, 1, 1, "空来源", nil); err == nil {
		t.Fatal("expected empty source rejection")
	}
	if _, err := CreateCaseFlow(db, 1, 1, "文档来源", []SourceInput{{Kind: "document", DocumentID: 1}}); err != nil {
		t.Fatalf("expected create success: %v", err)
	}
}

func TestCaseFlowDraftVersionAndRestore(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "版本流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := getCaseFlowDraft(db, cf.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree := caseflow.Tree{Root: &caseflow.Node{ID: "r", Title: "根", Status: caseflow.StatusUncovered}}
	if _, err := UpdateCaseFlowDraft(db, cf.ID, d.Revision, treeJSON(tree)); err != nil {
		t.Fatal(err)
	}
	v1, err := SaveCaseFlowVersion(db, cf.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.VersionNo != 1 {
		t.Fatalf("version no = %d", v1.VersionNo)
	}

	// 修改草稿再恢复版本
	d2, _ := getCaseFlowDraft(db, cf.ID)
	modified := caseflow.Tree{Root: &caseflow.Node{ID: "r", Title: "改", Status: caseflow.StatusCovered}}
	if _, err := UpdateCaseFlowDraft(db, cf.ID, d2.Revision, treeJSON(modified)); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreCaseFlowVersion(db, cf.ID, 1); err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	if view.Tree.Root.Title != "根" {
		t.Fatalf("restore did not apply: %s", view.Tree.Root.Title)
	}
	versions, _ := ListCaseFlowVersions(db, cf.ID)
	if len(versions) != 1 || versions[0].Tree == "" {
		t.Fatal("version should remain unchanged")
	}
}

func TestResolveCaseSourcesSnapshotsTags(t *testing.T) {
	db := setupCaseFlowDB(t)
	db.Create(&model.TestUnit{TestSetID: 1, Method: "GET", Path: "/a", Slug: "get-a", Tag: "用户"})
	db.Create(&model.TestUnit{TestSetID: 1, Method: "GET", Path: "/b", Slug: "get-b", Tag: "订单"})
	cf, err := CreateCaseFlow(db, 1, 1, "tag流", []SourceInput{{Kind: "tag", Tags: []string{"用户"}}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveCaseSources(db, cf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || len(resolved[0].UnitIDs) != 1 || resolved[0].UnitIDs[0] != 1 {
		t.Fatalf("unexpected tag resolution: %#v", resolved)
	}
	// Tag 内容变化不应改变已保存的树。
	view, _ := GetCaseTreeView(db, cf.ID)
	before := view.Tree.Root.Title
	db.Model(&model.TestUnit{}).Where("id = ?", 1).Update("tag", "新标签")
	view2, _ := GetCaseTreeView(db, cf.ID)
	if view2.Tree.Root.Title != before {
		t.Fatal("source change mutated tree")
	}
}

func TestCaseFlowDraftRevisionConflict(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "并发流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := getCaseFlowDraft(db, cf.ID)
	tree := caseflow.Tree{Root: &caseflow.Node{ID: "r", Title: "A", Status: caseflow.StatusUncovered}}
	if _, err := UpdateCaseFlowDraft(db, cf.ID, d.Revision, treeJSON(tree)); err != nil {
		t.Fatal(err)
	}
	// 使用过期 revision 应被拒绝。
	if _, err := UpdateCaseFlowDraft(db, cf.ID, d.Revision, treeJSON(tree)); err == nil {
		t.Fatal("expected stale revision rejection")
	}
}

func TestCaseNodeOperationsRejectCycle(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "节点流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	rootID := view.Tree.Root.ID
	d, err := AddCaseNode(db, cf.ID, view.Draft.Revision, rootID, "子节点")
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := parseCaseTree(d.Tree)
	childID := tree.Root.Children[0].ID
	// 将根移动到其子节点下会形成环，应被拒绝。
	if _, err := MoveCaseNode(db, cf.ID, d.Revision, rootID, childID); err == nil {
		t.Fatal("expected cycle rejection")
	}
}
