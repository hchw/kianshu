package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateCaseFlowSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model-case-schema?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"background_documents", "case_flows", "case_flow_drafts", "case_flow_versions", "case_sources", "case_nodes", "case_coverages"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing migrated table %s", table)
		}
	}
}
