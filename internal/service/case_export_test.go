package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
)

func TestExportCaseFlowXMindDraftAndVersion(t *testing.T) {
	db := setupCaseFlowDB(t)
	cf, err := CreateCaseFlow(db, 1, 1, "导出流", []SourceInput{{Kind: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := GetCaseTreeView(db, cf.ID)
	rootID := view.Tree.Root.ID
	_, err = AddCaseNode(db, cf.ID, view.Draft.Revision, rootID, "登录用例")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCaseFlowVersion(db, cf.ID, 1); err != nil {
		t.Fatal(err)
	}

	data, name, err := ExportCaseFlowXMind(db, cf.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Fatal("empty filename")
	}
	assertXMindContains(t, data, "登录用例")

	versionNo := 1
	data, _, err = ExportCaseFlowXMind(db, cf.ID, &versionNo)
	if err != nil {
		t.Fatal(err)
	}
	assertXMindContains(t, data, "登录用例")
}

func assertXMindContains(t *testing.T, data []byte, want string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var content []byte
	for _, f := range zr.File {
		if f.Name != "content.json" {
			continue
		}
		rc, _ := f.Open()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(rc)
		content = buf.Bytes()
		rc.Close()
	}
	var sheets []map[string]any
	if err := json.Unmarshal(content, &sheets); err != nil {
		t.Fatal(err)
	}
	if len(sheets) == 0 {
		t.Fatal("no sheets")
	}
	if !bytes.Contains(content, []byte(want)) {
		t.Fatalf("content missing %q: %s", want, content)
	}
}
