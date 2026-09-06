package service

import (
	"testing"

	"github/hchw/kianshu/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBackgroundDocumentCRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	u := &model.User{Username: "u", PasswordHash: "x"}
	db.Create(u)
	ts := &model.TestSet{Name: "ts", OwnerID: u.ID}
	db.Create(ts)

	doc, err := CreateBackgroundDocument(db, ts.ID, u.ID, "需求", "内容")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBackgroundDocument(db, ts.ID, u.ID, "", ""); err == nil {
		t.Fatal("expected empty rejection")
	}
	if got, _ := GetBackgroundDocument(db, ts.ID, doc.ID); got.Name != "需求" {
		t.Fatal("get failed")
	}
	name := "新需求"
	if _, err := UpdateBackgroundDocument(db, ts.ID, doc.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBackgroundDocument(db, ts.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetBackgroundDocument(db, ts.ID, doc.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}
