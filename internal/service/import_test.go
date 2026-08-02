package service

import (
	"testing"

	"github/hchw/kianshu/internal/model"
)

func TestParseSwagger_StandardV2(t *testing.T) {
	data := `{"swagger":"2.0","info":{"title":"T"},"paths":{"/pets":{"get":{"summary":"List pets","operationId":"listPets","responses":{"200":{"description":"OK"}}}}}}`
	doc, issues, err := ParseSwagger([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	if len(doc.Operations) != 1 {
		t.Fatalf("expected 1 op, got %d", len(doc.Operations))
	}
	if doc.Operations[0].Method != "GET" || doc.Operations[0].Path != "/pets" {
		t.Fatalf("wrong op: %s %s", doc.Operations[0].Method, doc.Operations[0].Path)
	}
}

func TestParseSwagger_UnrecognizedVersion(t *testing.T) {
	data := `{"swagger":"1.0","paths":{"/api":{"get":{"summary":"API","responses":{"200":{}}}}}}`
	doc, issues, err := ParseSwagger([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %v", issues)
	}
	if len(doc.Operations) != 1 {
		t.Fatalf("expected 1 op from lenient parse, got %d", len(doc.Operations))
	}
}

func TestParseSwagger_Empty(t *testing.T) {
	_, _, err := ParseSwagger([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty doc")
	}
}

func TestParseSwagger_NotJSON(t *testing.T) {
	_, _, err := ParseSwagger([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for non-JSON")
	}
}

func TestParseSwagger_NoPaths(t *testing.T) {
	_, _, err := ParseSwagger([]byte(`{"swagger":"2.0","info":{}}`))
	if err == nil {
		t.Fatal("expected error for missing paths")
	}
}

func TestParseSwagger_NoOps(t *testing.T) {
	_, _, err := ParseSwagger([]byte(`{"swagger":"2.0","paths":{}}`))
	if err == nil {
		t.Fatal("expected error for zero ops")
	}
}

func TestImportSwagger_NeedConfirmation(t *testing.T) {
	gdb := testDB(t)
	ts := &model.TestSet{Name: "ts1", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	data := `{"swagger":"1.0","paths":{"/api":{"get":{"summary":"API","responses":{"200":{}}}}}}`
	_, err := ImportSwagger(gdb, ts.ID, "test", []byte(data), false)
	if err == nil {
		t.Fatal("expected NeedConfirmationError")
	}
	nce, ok := err.(*NeedConfirmationError)
	if !ok {
		t.Fatalf("expected NeedConfirmationError, got %T: %v", err, err)
	}
	if len(nce.Issues) == 0 {
		t.Fatal("expected issues in NeedConfirmationError")
	}
}

func TestImportSwagger_ConfirmCreatesUnits(t *testing.T) {
	gdb := testDB(t)
	ts := &model.TestSet{Name: "ts2", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	data := `{"swagger":"1.0","paths":{"/api":{"get":{"summary":"API","responses":{"200":{}}}}}}`
	res, err := ImportSwagger(gdb, ts.ID, "test", []byte(data), true)
	if err != nil {
		t.Fatalf("expected success with confirm, got: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("expected 1 created, got %d", res.Created)
	}
	var count int64
	gdb.Model(&model.TestUnit{}).Where("test_set_id = ?", ts.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 unit, got %d", count)
	}
}

func TestImportSwagger_StandardDirectImport(t *testing.T) {
	gdb := testDB(t)
	ts := &model.TestSet{Name: "ts3", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	data := `{"swagger":"2.0","info":{"title":"T"},"paths":{"/pets":{"get":{"summary":"Pets","responses":{"200":{"description":"OK"}}}}}}`
	res, err := ImportSwagger(gdb, ts.ID, "test", []byte(data), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("expected 1 created, got %d", res.Created)
	}
}

func TestImportSwagger_InvalidDocument(t *testing.T) {
	gdb := testDB(t)
	ts := &model.TestSet{Name: "ts4", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	_, err := ImportSwagger(gdb, ts.ID, "test", []byte(""), true)
	if err == nil {
		t.Fatal("expected error for empty doc")
	}
}

func TestImportSwagger_V3Strict(t *testing.T) {
	gdb := testDB(t)
	ts := &model.TestSet{Name: "ts5", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	data := `{"openapi":"3.0.0","info":{"title":"T"},"paths":{"/users":{"get":{"summary":"Users","responses":{"200":{"description":"OK"}}}}}}`
	res, err := ImportSwagger(gdb, ts.ID, "test", []byte(data), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("expected 1 created, got %d", res.Created)
	}
}
