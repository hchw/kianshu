package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github/hchw/kianshu/internal/model"
	"gorm.io/gorm"
)

func TestDashboardScopesSharedResources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:dashboard-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	owner := model.User{Username: "owner", PasswordHash: "x"}
	viewer := model.User{Username: "viewer", PasswordHash: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}
	owned := model.TestSet{Name: "owned", OwnerID: owner.ID}
	shared := model.TestSet{Name: "shared", OwnerID: owner.ID}
	hidden := model.TestSet{Name: "hidden", OwnerID: owner.ID}
	for _, s := range []*model.TestSet{&owned, &shared, &hidden} {
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.TestSetMember{TestSetID: shared.ID, UserID: viewer.ID, Role: model.RoleRead}).Error; err != nil {
		t.Fatal(err)
	}
	data, err := Dashboard(db, viewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data.Summary.TestSets != 1 {
		t.Fatalf("got %d test sets, want 1", data.Summary.TestSets)
	}
	if len(data.RecentWork) != 1 || data.RecentWork[0].Name != "shared" || data.RecentWork[0].Role != "read" {
		t.Fatalf("unexpected recent work: %#v", data.RecentWork)
	}
	for _, item := range data.RecentWork {
		if item.Name == "hidden" || item.Name == "owned" {
			t.Fatalf("private resource leaked: %#v", item)
		}
	}
}
