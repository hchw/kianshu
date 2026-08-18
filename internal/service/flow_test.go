package service

import (
	"encoding/json"
	"errors"
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

func TestDeriveInputs(t *testing.T) {
	unit := model.TestUnit{
		ID:     1,
		Method: "POST",
		Path:   "/login",
		Params: `[{"name":"Authorization","in":"header","type":"string"},{"name":"id","in":"path","type":"integer","required":true}]`,
		RequestBody: `{"type":"object","properties":{"username":{"type":"string"},"password":{"type":"string"}},"required":["username","password"]}`,
	}

	io := deriveInputs(unit)
	t.Logf("derived inputs: %+v", io)

	// Auth key pre-wired to $cache.token
	if v, ok := io["Authorization"]; !ok {
		t.Fatal("missing Authorization key")
	} else if v.Type != flow.IOTypePrimitive {
		t.Fatalf("Authorization type: got %s, want primitive", v.Type)
	} else if v.Source != "$cache.token" {
		t.Fatalf("Authorization source: got %q, want $cache.token", v.Source)
	}

	// Non-auth param not pre-filled with source
	if v, ok := io["id"]; !ok {
		t.Fatal("missing id key")
	} else if v.Type != flow.IOTypePrimitive {
		t.Fatalf("id type: got %s, want primitive", v.Type)
	} else if v.Source != "" {
		t.Fatalf("id source: got %q, want empty (non-auth params not pre-wired)", v.Source)
	}

	// Body properties extracted
	if v, ok := io["username"]; !ok {
		t.Fatal("missing username key from request_body")
	} else if v.Source != "" {
		t.Fatalf("username source: got %q, want empty", v.Source)
	}
	if _, ok := io["password"]; !ok {
		t.Fatal("missing password key from request_body")
	}
}

func TestDeriveInputsEmptyAndNull(t *testing.T) {
	tests := []struct {
		name   string
		params string
		body   string
	}{
		{"empty strings", "", ""},
		{"null strings", "null", "null"},
		{"whitespace", "  ", "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := model.TestUnit{ID: 2, Params: tt.params, RequestBody: tt.body}
			io := deriveInputs(u)
			if len(io) != 0 {
				t.Fatalf("expected empty inputs, got %+v", io)
			}
		})
	}
}

func TestDeriveInputsParseFailure(t *testing.T) {
	u := model.TestUnit{ID: 3, Params: `not-json`, RequestBody: `also-not-json`}
	io := deriveInputs(u)
	if len(io) != 0 {
		t.Fatalf("parse failures should yield empty inputs, got %+v", io)
	}
}

func TestDeriveInputsObjectType(t *testing.T) {
	u := model.TestUnit{
		ID:   4,
		Params: `[{"name":"config","in":"body","type":"object"}]`,
		RequestBody: `{"type":"object","properties":{"nested":{"type":"object"},"items":{"type":"array"}}}`,
	}
	io := deriveInputs(u)
	if v, ok := io["config"]; !ok || v.Type != flow.IOTypeObject {
		t.Fatalf("expected object type for config, got %+v", v)
	}
	if v, ok := io["nested"]; !ok || v.Type != flow.IOTypeObject {
		t.Fatalf("expected object type for nested, got %+v", v)
	}
	if v, ok := io["items"]; !ok || v.Type != flow.IOTypeArray {
		t.Fatalf("expected array type for items, got %+v", v)
	}
}

func TestDeriveInputsParamsOverrideBody(t *testing.T) {
	// Same key in both params and request_body → params wins.
	u := model.TestUnit{
		ID:   5,
		Params: `[{"name":"email","in":"query","type":"string"}]`,
		RequestBody: `{"type":"object","properties":{"email":{"type":"integer"}}}`,
	}
	io := deriveInputs(u)
	// email should be string (from params), not integer (from body)
	if v, ok := io["email"]; !ok {
		t.Fatal("missing email")
	} else if v.Type != flow.IOTypePrimitive {
		t.Fatalf("email type: got %s, want primitive (params win over body)", v.Type)
	}
}

func TestSnapshotAPINodesWithDeriveInputs(t *testing.T) {
	gdb := testDB(t)
	unit := &model.TestUnit{
		TestSetID: 1, Method: "POST", Path: "/login", Slug: "post-login",
		Name: "登录", Params: `[{"name":"Authorization","in":"header","type":"string"},{"name":"username","in":"query","type":"string"}]`,
		Spec: `{"paths":{"/login":{"post":{}}}}`,
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

	n2 := tree.Nodes["n2"]
	if len(n2.Inputs) == 0 {
		t.Fatal("expected inputs to be auto-populated, got empty")
	}
	if v, ok := n2.Inputs["Authorization"]; !ok || v.Source != "$cache.token" {
		t.Fatalf("Authorization input: %+v (want source=$cache.token)", v)
	}
	// 无 source 的 input（如 query 参数 username）不再注入，避免校验假阳性。
	if _, ok := n2.Inputs["username"]; ok {
		t.Fatalf("username should NOT be injected (no source), got %+v", n2.Inputs["username"])
	}

	// Config snapshot still works
	var out struct {
		UnitID uint `json:"unit_id"`
		Unit   struct {
			Method string
		}
	}
	if err := flow.UnmarshalConfig(n2, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.UnitID != unit.ID || out.Unit.Method != "POST" {
		t.Fatalf("snapshot not captured: %+v", out)
	}
}

func TestSnapshotAPINodesInputsProtection(t *testing.T) {
	gdb := testDB(t)
	unit := &model.TestUnit{
		TestSetID: 1, Method: "GET", Path: "/users/{id}", Slug: "get-users-id",
		Name: "获取用户", Params: `[{"name":"Authorization","in":"header","type":"string"},{"name":"id","in":"path","type":"string"}]`,
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
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"Authorization": {Type: flow.IOTypePrimitive, Source: "$cache.custom_token"},
	}
	tree.AddChild("n1", "n2")

	if err := SnapshotAPINodes(gdb, tree); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	n2 := tree.Nodes["n2"]
	// Authorization source should NOT be overwritten
	if v, ok := n2.Inputs["Authorization"]; !ok || v.Source != "$cache.custom_token" {
		t.Fatalf("Authorization source was overwritten: %+v", v)
	}
	// 无 source 的 input（如 path 参数 id）不再注入。
	if _, ok := n2.Inputs["id"]; ok {
		t.Fatal("id should NOT be injected (no source)")
	}
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

	logs, _, err := ListRuns(gdb, flowID, 1, 20)
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
	if out.StatusCode != 200 {
		t.Fatalf("status_code = %v, want 200", out.StatusCode)
	}
	body, _ := out.Body.(map[string]any)
	if body == nil || body["token"] != "abc" {
		t.Fatalf("body.token mismatch: %v", out)
	}
}

func TestHTTPCallAPIPassesThrough4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`unauthorized`))
	}))
	defer srv.Close()

	callAPI := HTTPCallAPI(srv.URL, 5*time.Second)
	out, err := callAPI(exec.APICall{Method: "GET", URL: srv.URL + "/x"})
	if err != nil {
		t.Fatalf("4xx 不应导致错误,错误码判断由断言节点负责: %v", err)
	}
	if out.StatusCode != 401 {
		t.Fatalf("status_code = %v, want 401", out.StatusCode)
	}
	if out.Body != "unauthorized" {
		t.Fatalf("body = %v, want unauthorized", out.Body)
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

// TestDeleteFlow verifies hard deletion cascades across draft/versions/runs/
// schedules, cancels registered jobs and rejects missing flows.
func TestDeleteFlow(t *testing.T) {
	gdb := testDB(t)
	f, err := CreateFlow(gdb, 1, 1, "待删流")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	if err := gdb.Create(&model.FlowVersion{FlowID: f.ID, VersionNo: 1, Tree: "{}", Enabled: true}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	if err := gdb.Create(&model.ExecutionLog{FlowID: f.ID, Status: "success", Tree: "{}"}).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	fs := newFakeScheduler()
	if err := gdb.Create(&model.FlowSchedule{FlowID: f.ID, TestSetID: 1, Cron: "0 * * * *", JobID: "job-1"}).Error; err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	mgr := NewScheduleManager(gdb, fs, time.Minute, nil)

	if err := DeleteFlow(gdb, mgr, f.ID); err != nil {
		t.Fatalf("delete flow: %v", err)
	}
	for _, m := range []any{&model.TestFlow{}, &model.FlowDraft{}, &model.FlowVersion{}, &model.ExecutionLog{}, &model.FlowSchedule{}} {
		var cnt int64
		if err := gdb.Model(m).Count(&cnt).Error; err != nil {
			t.Fatalf("count %T: %v", m, err)
		}
		if cnt != 0 {
			t.Fatalf("expected 0 rows in %T after delete, got %d", m, cnt)
		}
	}
	if len(fs.removed) != 1 || fs.removed[0] != "job-1" {
		t.Fatalf("expected scheduler.Remove(job-1), got %v", fs.removed)
	}
	if err := DeleteFlow(gdb, mgr, f.ID); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("expected ErrFlowNotFound on second delete, got %v", err)
	}
}

// TestDuplicateFlow verifies DuplicateFlow clones the current draft into a new
// flow with derived or custom names, and rejects missing source flows.
func TestDuplicateFlow(t *testing.T) {
	gdb := testDB(t)
	f, err := CreateFlow(gdb, 1, 1, "支付流程")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	// 写入一棵非空树作为工作状态。
	tree := `{"start":"n1","nodes":{"n1":{"id":"n1","type":"start"}}}`
	if _, err := UpdateDraft(gdb, f.ID, "支付流程", tree); err != nil {
		t.Fatalf("update draft: %v", err)
	}

	t.Run("default name derived", func(t *testing.T) {
		dup, err := DuplicateFlow(gdb, f.ID, 2, "")
		if err != nil {
			t.Fatalf("duplicate: %v", err)
		}
		if dup.ID == f.ID {
			t.Fatal("duplicated flow must have a new ID")
		}
		if dup.TestSetID != f.TestSetID {
			t.Fatalf("test set: got %d want %d", dup.TestSetID, f.TestSetID)
		}
		if dup.Name != "支付流程 副本" {
			t.Fatalf("name: got %q want %q", dup.Name, "支付流程 副本")
		}
		if dup.CreatedBy != 2 {
			t.Fatalf("created_by: got %d want 2", dup.CreatedBy)
		}
		d, err := GetDraft(gdb, dup.ID)
		if err != nil {
			t.Fatalf("get duplicated draft: %v", err)
		}
		if d.Name != dup.Name {
			t.Fatalf("draft name: got %q want %q", d.Name, dup.Name)
		}
		if d.Tree != tree {
			t.Fatalf("draft tree: got %q want %q", d.Tree, tree)
		}
		// 复制必须独立:改新流草稿不影响原流。
		if _, err := UpdateDraft(gdb, dup.ID, dup.Name, `{"start":"n2","nodes":{"n2":{"id":"n2","type":"start"}}}`); err != nil {
			t.Fatalf("update dup draft: %v", err)
		}
		orig, err := GetDraft(gdb, f.ID)
		if err != nil {
			t.Fatalf("get original draft: %v", err)
		}
		if orig.Tree != tree {
			t.Fatal("original draft must be unaffected by duplicate edits")
		}
	})

	t.Run("custom name", func(t *testing.T) {
		dup, err := DuplicateFlow(gdb, f.ID, 1, "回归基线")
		if err != nil {
			t.Fatalf("duplicate: %v", err)
		}
		if dup.Name != "回归基线" {
			t.Fatalf("name: got %q want %q", dup.Name, "回归基线")
		}
	})

	t.Run("missing flow", func(t *testing.T) {
		if _, err := DuplicateFlow(gdb, 9999, 1, ""); !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("empty draft still copies", func(t *testing.T) {
		f2, err := CreateFlow(gdb, 1, 1, "空流")
		if err != nil {
			t.Fatalf("create flow: %v", err)
		}
		if err := gdb.Where("flow_id = ?", f2.ID).Delete(&model.FlowDraft{}).Error; err != nil {
			t.Fatalf("delete draft: %v", err)
		}
		dup, err := DuplicateFlow(gdb, f2.ID, 1, "")
		if err != nil {
			t.Fatalf("duplicate empty-draft flow: %v", err)
		}
		d, err := GetDraft(gdb, dup.ID)
		if err != nil {
			t.Fatalf("get draft: %v", err)
		}
		if d.Tree != "" {
			t.Fatalf("expected empty tree, got %q", d.Tree)
		}
	})
}

// TestRenameFlow verifies renaming keeps TestFlow.Name and FlowDraft.Name in
// sync, and tolerates a missing draft.
func TestRenameFlow(t *testing.T) {
	gdb := testDB(t)
	f, err := CreateFlow(gdb, 1, 1, "旧名字")
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	renamed, err := RenameFlow(gdb, f.ID, "新名字")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "新名字" {
		t.Fatalf("flow name: got %q want %q", renamed.Name, "新名字")
	}
	var flowRow model.TestFlow
	if err := gdb.First(&flowRow, f.ID).Error; err != nil {
		t.Fatalf("reload flow: %v", err)
	}
	if flowRow.Name != "新名字" {
		t.Fatalf("persisted flow name: got %q", flowRow.Name)
	}
	d, err := GetDraft(gdb, f.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if d.Name != "新名字" {
		t.Fatalf("draft name: got %q want %q", d.Name, "新名字")
	}

	t.Run("missing draft tolerated", func(t *testing.T) {
		f2, err := CreateFlow(gdb, 1, 1, "无草稿")
		if err != nil {
			t.Fatalf("create flow: %v", err)
		}
		if err := gdb.Where("flow_id = ?", f2.ID).Delete(&model.FlowDraft{}).Error; err != nil {
			t.Fatalf("delete draft: %v", err)
		}
		if _, err := RenameFlow(gdb, f2.ID, "改名成功"); err != nil {
			t.Fatalf("rename without draft: %v", err)
		}
		var flowRow model.TestFlow
		if err := gdb.First(&flowRow, f2.ID).Error; err != nil {
			t.Fatalf("reload flow: %v", err)
		}
		if flowRow.Name != "改名成功" {
			t.Fatalf("flow name: got %q", flowRow.Name)
		}
	})

	t.Run("missing flow", func(t *testing.T) {
		if _, err := RenameFlow(gdb, 9999, "x"); !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound, got %v", err)
		}
	})
}
