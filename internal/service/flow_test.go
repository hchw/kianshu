package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github/hchw/kianshu/internal/config"
	"github/hchw/kianshu/internal/db"
	"github/hchw/kianshu/internal/exec"
	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("KS_ENC_KEY", "dev-only-32byte-secret-key-00001")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DBDriver = "sqlite"
	cfg.DSN = ":memory:"
	gdb, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

func TestSnapshotAPINodes(t *testing.T) {
	gdb := testDB(t)
	unit := &model.TestUnit{
		TestSetID: 1, Method: "POST", Path: "/login", Slug: "post-login",
		Name: "登录", Spec: `{"paths":{"/login":{"post":{}}}}`,
	}
	if err := gdb.Create(unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}

	tree := &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
			"n2": flow.NewNode("n2", flow.NodeAPI),
		},
	}
	cfg, err := flow.MarshalConfig(map[string]any{"unit_id": unit.ID})
	if err != nil {
		t.Fatal(err)
	}
	tree.Nodes["n2"].Config = cfg
	tree.AddChild("n1", "n2")

	if err := SnapshotAPINodes(gdb, tree); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var out struct {
		UnitID uint `json:"unit_id"`
		Unit   struct {
			Method string
			Path   string
			Spec   string
		}
	}
	if err := flow.UnmarshalConfig(tree.Nodes["n2"], &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.UnitID != unit.ID || out.Unit.Method != "POST" || out.Unit.Path != "/login" || out.Unit.Spec == "" {
		t.Fatalf("snapshot not captured: %+v", out)
	}
}

func TestSoftDeleteUnitWarning(t *testing.T) {
	gdb := testDB(t)
	unit := &model.TestUnit{TestSetID: 1, Method: "GET", Path: "/x"}
	if err := gdb.Create(unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}
	if err := gdb.Delete(unit).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	tree := &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
			"n2": flow.NewNode("n2", flow.NodeAPI),
		},
	}
	cfg, err := flow.MarshalConfig(map[string]any{"unit_id": unit.ID})
	if err != nil {
		t.Fatal(err)
	}
	tree.Nodes["n2"].Config = cfg
	tree.AddChild("n1", "n2")

	res := flow.Validate(tree, flow.ValidatorOptions{
		UnitDeleted: func(uid uint) bool {
			var u model.TestUnit
			return gdb.Unscoped().First(&u, uid).Error == nil && u.DeletedAt.Valid
		},
	})
	if len(res.Warnings) == 0 {
		t.Fatalf("expected soft-delete warning, got %+v", res)
	}
}

// setupFlow creates a test set, a flow, and a test unit for run tests.
func setupFlow(t *testing.T, gdb *gorm.DB, host string) (flowID uint, tree *flow.Tree) {
	t.Helper()
	ts := &model.TestSet{Name: "ts", OwnerID: 1, Host: host}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	unit := &model.TestUnit{TestSetID: ts.ID, Method: "POST", Path: "/login", Slug: "post-login", Name: "登录"}
	if err := gdb.Create(unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}
	f, err := CreateFlow(gdb, ts.ID, 1, "flow")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	tree = &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
			"n2": flow.NewNode("n2", flow.NodeAPI),
		},
	}
	cfg, err := flow.MarshalConfig(map[string]any{"unit_id": unit.ID})
	if err != nil {
		t.Fatal(err)
	}
	tree.Nodes["n2"].Config = cfg
	tree.AddChild("n1", "n2")
	return f.ID, tree
}

func TestTrialRunRecordsLog(t *testing.T) {
	gdb := testDB(t)
	flowID, tree := setupFlow(t, gdb, "http://example.com")
	d := &model.FlowDraft{FlowID: flowID, Name: "flow", Tree: tree.String()}
	if err := gdb.Create(d).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	log, err := TrialRun(gdb, flowID, 30*time.Second)
	if err != nil {
		t.Fatalf("trial run: %v", err)
	}
	if log.VersionID != 0 {
		t.Fatalf("trial run should bind version_id 0, got %d", log.VersionID)
	}
	if log.Status == "" {
		t.Fatal("trial run should record a status")
	}
	if log.Tree == "" || log.NodeResults == "" {
		t.Fatal("trial run should persist tree and node_results")
	}
	var results map[string]any
	if err := json.Unmarshal([]byte(log.NodeResults), &results); err != nil {
		t.Fatalf("node_results should be JSON: %v", err)
	}
	if _, ok := results["n1"]; !ok {
		t.Fatalf("node_results should include the start node, got %v", results)
	}
}

func TestRunVersionBindsVersionID(t *testing.T) {
	gdb := testDB(t)
	flowID, tree := setupFlow(t, gdb, "http://example.com")
	v, _, err := SaveAndEnable(gdb, flowID, 1)
	if err != nil {
		t.Fatalf("save and enable: %v", err)
	}
	_ = tree

	log, err := RunVersion(gdb, flowID, v.VersionNo, 30*time.Second)
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if log.VersionID != v.ID || log.VersionNo != v.VersionNo {
		t.Fatalf("version run should bind version id %d no %d, got %d/%d", v.ID, v.VersionNo, log.VersionID, log.VersionNo)
	}

	logs, err := ListRuns(gdb, flowID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	got, err := GetRun(gdb, logs[0].ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.ID != log.ID {
		t.Fatalf("get run returned wrong log: %d", got.ID)
	}
}

func TestHTTPCallAPIBuildsRequest(t *testing.T) {
	var gotHeader string
	var gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"abc"}`))
	}))
	defer srv.Close()

	callAPI := HTTPCallAPI(srv.URL, 5*time.Second)
	out, err := callAPI(exec.APICall{
		Method:  "POST",
		URL:     srv.URL + "/login",
		Headers: map[string]string{"Authorization": "Bearer x"},
		Query:   map[string]any{"scope": "read"},
		Body:    map[string]any{"account": "a"},
	})
	if err != nil {
		t.Fatalf("call api: %v", err)
	}
	if gotHeader != "Bearer x" {
		t.Fatalf("header mismatch: %q", gotHeader)
	}
	if gotQuery != "scope=read" {
		t.Fatalf("query mismatch: %q", gotQuery)
	}
	if gotBody["account"] != "a" {
		t.Fatalf("body mismatch: %v", gotBody)
	}
	if m, ok := out.(map[string]any); !ok || m["token"] != "abc" {
		t.Fatalf("output mismatch: %v", out)
	}
}

func TestHTTPCallAPIErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`unauthorized`))
	}))
	defer srv.Close()

	callAPI := HTTPCallAPI(srv.URL, 5*time.Second)
	if _, err := callAPI(exec.APICall{Method: "GET", URL: srv.URL + "/x"}); err == nil {
		t.Fatal("expected error for 4xx response")
	}
}

func TestHTTPCallAPITimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	start := time.Now()
	callAPI := HTTPCallAPI(srv.URL, 50*time.Millisecond)
	_, err := callAPI(exec.APICall{Method: "GET", URL: srv.URL + "/slow"})
	if err == nil {
		t.Fatal("expected timeout error for slow upstream")
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("timeout call took %v, want bounded", elapsed)
	}
}

func TestConcurrentSaveAndEnableDistinctVersionNos(t *testing.T) {
	t.Setenv("KS_ENC_KEY", "dev-only-32byte-secret-key-00001")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DBDriver = "sqlite"
	// File-backed DB (not shared-cache memory): real rollback-journal locking so
	// concurrent writers serialize via busy_timeout, exercising the transaction
	// path.
	cfg.DSN = "file:" + t.TempDir() + "/kianshu.db?_busy_timeout=10000"
	gdb, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ts := &model.TestSet{Name: "ts", OwnerID: 1}
	if err := gdb.Create(ts).Error; err != nil {
		t.Fatalf("create test set: %v", err)
	}
	unit := &model.TestUnit{TestSetID: ts.ID, Method: "POST", Path: "/login", Slug: "post-login", Name: "登录"}
	if err := gdb.Create(unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}
	f, err := CreateFlow(gdb, ts.ID, 1, "flow")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	tree := &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
			"n2": flow.NewNode("n2", flow.NodeAPI),
		},
	}
	d := &model.FlowDraft{FlowID: f.ID, Name: "flow", Tree: tree.String()}
	if err := gdb.Create(d).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	nos := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, _, err := SaveAndEnable(gdb, f.ID, 1)
			errs[idx] = err
			if err == nil {
				nos[idx] = v.VersionNo
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent save %d failed: %v", i, errs[i])
		}
	}
	seen := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		if nos[i] == 0 {
			t.Fatalf("concurrent save %d produced version 0", i)
		}
		if seen[nos[i]] {
			t.Fatalf("duplicate version_no %d from concurrent saves", nos[i])
		}
		seen[nos[i]] = true
	}
	var cnt int64
	if err := gdb.Model(&model.FlowVersion{}).Where("flow_id = ?", f.ID).Count(&cnt).Error; err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if int(cnt) != n {
		t.Fatalf("expected %d versions, got %d", n, cnt)
	}
}
