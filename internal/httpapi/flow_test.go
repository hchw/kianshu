package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github/hchw/kianshu/internal/config"
	"github/hchw/kianshu/internal/db"
	"github/hchw/kianshu/internal/httpapi"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("KS_ENC_KEY", "dev-only-32byte-secret-key-00001")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.DBDriver = "sqlite"
	cfg.DSN = ":memory:"
	cfg.EncKey = []byte("dev-only-32byte-secret-key-00001")
	gdb, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

type client struct {
	t   *testing.T
	ts  *httptest.Server
	tok string
}

// sharedServer starts one server over a shared in-memory DB, so tests can
// exercise multi-user permissions against the same data.
func sharedServer(t *testing.T) *httptest.Server {
	t.Helper()
	gdb := newTestDB(t)
	srv, err := httpapi.New(gdb, &config.Config{
		EncKey:     []byte("dev-only-32byte-secret-key-00001"),
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func newClient(t *testing.T) *client {
	return &client{t: t, ts: sharedServer(t)}
}

func (c *client) do(method, path, body string, want int) (int, map[string]any) {
	c.t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.tok != "" {
		req.Header.Set("Authorization", "Bearer "+c.tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: got %d want %d, body=%v", method, path, resp.StatusCode, want, out)
	}
	return resp.StatusCode, out
}

func (c *client) registerAndLogin() uint {
	c.t.Helper()
	return c.registerAndLoginAs("alice")
}

func (c *client) registerAndLoginAs(username string) uint {
	c.t.Helper()
	_, _ = c.do("POST", "/api/auth/register", fmt.Sprintf(`{"username":"%s","password":"secret1"}`, username), http.StatusCreated)
	_, out := c.do("POST", "/api/auth/login", fmt.Sprintf(`{"username":"%s","password":"secret1"}`, username), http.StatusOK)
	c.tok = out["token"].(string)
	return uint(out["id"].(float64))
}

func TestFlowLifecycle(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()

	// Create a test set and a flow inside it.
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"my flow","system_prompt":"签名规则 v0"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// Read the initial draft.
	_, draft := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if !strings.Contains(draft["tree"].(string), `"type":"start"`) {
		t.Fatalf("initial draft should contain a start node, got %v", draft["tree"])
	}
	if draft["system_prompt"] != "签名规则 v0" {
		t.Fatalf("create should persist system_prompt, got %v", draft["system_prompt"])
	}

	// Build a valid tree: start -> cache-set(writes authorization) -> api(reader).
	tree := `{"start":"n1","nodes":{
	  "n1":{"id":"n1","type":"start","outputs":{"token":{"type":"primitive"}},"config":{"params":{"account":"a","password":"p"}}},
	  "cs1":{"id":"cs1","type":"cache-set","parent":"n1","inputs":{},"outputs":{},"config":{"writes":{"authorization":"$.token"}}},
	  "n2":{"id":"n2","type":"api","parent":"cs1","inputs":{"authorization":{"type":"primitive","source":"$cache.authorization"}},"outputs":{"data":{"type":"object"}},"config":{"unit_id":0}}
	}}`
	_, updated := c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"my flow","system_prompt":"签名规则 v1","tree":%s}`, mustJSON(t, tree)), http.StatusOK)
	_ = updated
	// 草稿 GET 返回最新文档；nil 字段（旧客户端）不覆盖已有文档。
	_, draft2 := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if draft2["system_prompt"] != "签名规则 v1" {
		t.Fatalf("draft GET should return system_prompt, got %v", draft2["system_prompt"])
	}

	// Validation should pass.
	_, vres := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/draft/validate", flowID), "", http.StatusOK)
	if n, ok := vres["errors"].([]any); ok && len(n) != 0 {
		t.Fatalf("expected valid tree, got errors: %v", vres)
	}

	// Save and enable -> creates version 1.
	_, ver := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusCreated)
	_ = ver

	// Version list should include version 1, enabled.
	_, versions := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusOK)
	list := versions["versions"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 version, got %d", len(list))
	}
	v1 := list[0].(map[string]any)
	if v1["system_prompt"] != "签名规则 v1" {
		t.Fatalf("version snapshot should carry system_prompt, got %v", v1["system_prompt"])
	}
	if v1["version_no"].(float64) != 1 {
		t.Fatalf("expected version_no 1, got %v", v1["version_no"])
	}

	// Snapshot restore: fetch version 1 and compare the tree.
	_, snap := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/versions/1", flowID), "", http.StatusOK)
	snapTree := snap["tree"].(string)
	if !strings.Contains(snapTree, `"authorization"`) {
		t.Fatalf("snapshot should contain authorization key, got %v", snapTree)
	}
	// Start node params are stored in plaintext in the snapshot.
	if !strings.Contains(snapTree, `"account":"a"`) {
		t.Fatalf("snapshot should keep start params in plaintext, got %v", snapTree)
	}

	// Enable another version promotes it and disables the previous one.
	tree2 := strings.Replace(tree, `"account":"a"`, `"account":"b"`, 1)
	c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"my flow","tree":%s}`, mustJSON(t, tree2)), http.StatusOK)
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusCreated)
	_, versions2 := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusOK)
	list2 := versions2["versions"].([]any)
	if len(list2) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(list2))
	}
}

func TestFlowList(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"flow a"}`, http.StatusCreated)
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"flow b"}`, http.StatusCreated)

	_, out := c.do("GET", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), "", http.StatusOK)
	list := out["flows"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(list))
	}
	first := list[0].(map[string]any)
	if first["name"].(string) != "flow a" {
		t.Fatalf("expected first flow 'flow a', got %v", first["name"])
	}

	// An unprivileged user cannot list another's flows.
	c2 := &client{t: t, ts: c.ts}
	c2.registerAndLoginAs("bob")
	c2.do("GET", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), "", http.StatusForbidden)
}

func TestFlowValidationBlocked(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"bad"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// Unresolved input key without a source should fail validation and block enabling.
	tree := `{"start":"n1","nodes":{
	  "n1":{"id":"n1","type":"start"},
	  "n2":{"id":"n2","type":"api","parent":"n1","inputs":{"user_id":{"type":"primitive","source":"nonexistent"}},"config":{"unit_id":0}}
	}}`
	c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"bad","tree":%s}`, mustJSON(t, tree)), http.StatusOK)
	_, vres := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/draft/validate", flowID), "", http.StatusOK)
	if n, ok := vres["errors"].([]any); !ok || len(n) == 0 {
		t.Fatalf("expected validation errors, got %v", vres)
	}
	// Enable must be rejected with 422.
	_, ver := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusUnprocessableEntity)
	_ = ver
}

func TestFlowPermissions(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"perm"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// A second, unprivileged user cannot read the flow's draft.
	c2 := &client{t: t, ts: c.ts}
	c2.registerAndLoginAs("bob")
	c2.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusForbidden)
	c2.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), `{"name":"x","tree":"{}"}`, http.StatusForbidden)
}

func TestFlowTrialRunAndLogs(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"run"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// A minimal valid tree: start node only.
	tree := `{"start":"n1","nodes":{"n1":{"id":"n1","type":"start","config":{"params":{"a":"1"}}}}}`
	c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"run","tree":%s}`, mustJSON(t, tree)), http.StatusOK)

	// Trial run executes the draft and records a log.
	_, log := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/draft/trial-run", flowID), "", http.StatusCreated)
	runID := uint(log["id"].(float64))
	if log["version_id"].(float64) != 0 {
		t.Fatalf("trial run should have version_id 0, got %v", log["version_id"])
	}
	if _, ok := log["node_results"].(string); !ok {
		t.Fatalf("trial run should persist node_results, got %v", log)
	}

	// Log list includes the trial run.
	_, runs := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/runs", flowID), "", http.StatusOK)
	list := runs["runs"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 run, got %d", len(list))
	}

	// Log detail returns the same run.
	_, detail := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/runs/%d", flowID, runID), "", http.StatusOK)
	if detail["id"].(float64) != float64(runID) {
		t.Fatalf("detail id mismatch: %v", detail["id"])
	}

	// A log belonging to another flow is not reachable through it.
	_, fl2 := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"other"}`, http.StatusCreated)
	flow2ID := uint(fl2["id"].(float64))
	c.do("GET", fmt.Sprintf("/api/flow/flows/%d/runs/%d", flow2ID, runID), "", http.StatusNotFound)
}

func TestFlowRunVersion(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"run"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	tree := `{"start":"n1","nodes":{"n1":{"id":"n1","type":"start","config":{"params":{"a":"1"}}}}}`
	c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"run","tree":%s}`, mustJSON(t, tree)), http.StatusOK)
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/versions", flowID), "", http.StatusCreated)

	// Execute version 1; the log binds the version id.
	_, log := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/versions/1/run", flowID), "", http.StatusCreated)
	if log["version_id"].(float64) == 0 {
		t.Fatalf("version run should bind a version id, got %v", log["version_id"])
	}
	if log["version_no"].(float64) != 1 {
		t.Fatalf("version_no mismatch: %v", log["version_no"])
	}

	// A read-only member may read logs but not run.
	c2 := &client{t: t, ts: c.ts}
	readerID := c2.registerAndLoginAs("reader")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/members", testSetID),
		fmt.Sprintf(`{"user_id":%d,"role":"read"}`, readerID), http.StatusCreated)
	c2.do("POST", fmt.Sprintf("/api/flow/flows/%d/draft/trial-run", flowID), "", http.StatusForbidden)
	c2.do("GET", fmt.Sprintf("/api/flow/flows/%d/runs", flowID), "", http.StatusOK)
}

func mustJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestDuplicateFlowAPI verifies the duplicate endpoint clones the current
// draft, supports custom names, tolerates an empty body (default name), and
// rejects read-only members.
func TestDuplicateFlowAPI(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"dup-demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"支付"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))
	tree := `{"start":"n1","nodes":{"n1":{"id":"n1","type":"start"}}}`
	c.do("PUT", fmt.Sprintf("/api/flow/flows/%d/draft", flowID),
		fmt.Sprintf(`{"name":"支付","tree":%s}`, mustJSON(t, tree)), http.StatusOK)

	// Default name derived when body is absent.
	_, dup := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/duplicate", flowID), "", http.StatusCreated)
	dupID := uint(dup["id"].(float64))
	if dup["name"] != "支付 副本" {
		t.Fatalf("expected default name 支付 副本, got %v", dup["name"])
	}
	_, d := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", dupID), "", http.StatusOK)
	if d["tree"] != tree {
		t.Fatalf("duplicated draft tree mismatch: got %v", d["tree"])
	}
	if d["name"] != "支付 副本" {
		t.Fatalf("duplicated draft name: got %v", d["name"])
	}

	// Custom name via body.
	_, dup2 := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/duplicate", flowID),
		`{"name":"回归基线"}`, http.StatusCreated)
	if dup2["name"] != "回归基线" {
		t.Fatalf("expected custom name, got %v", dup2["name"])
	}

	// Missing source flow -> 404.
	c.do("POST", "/api/flow/flows/9999/duplicate", "", http.StatusNotFound)

	// Read-only member cannot duplicate.
	c2 := &client{t: t, ts: c.ts}
	readerID := c2.registerAndLoginAs("dupreader")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/members", testSetID),
		fmt.Sprintf(`{"user_id":%d,"role":"read"}`, readerID), http.StatusCreated)
	c2.do("POST", fmt.Sprintf("/api/flow/flows/%d/duplicate", flowID), "", http.StatusForbidden)
}

// TestRenameFlowAPI verifies the rename endpoint keeps list and draft names in
// sync, rejects empty names, and forbids read-only members.
func TestRenameFlowAPI(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"rename-demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"旧名"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	_, renamed := c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d", flowID), `{"name":"新名"}`, http.StatusOK)
	if renamed["name"] != "新名" {
		t.Fatalf("expected new name, got %v", renamed["name"])
	}
	// Draft name follows.
	_, d := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if d["name"] != "新名" {
		t.Fatalf("draft name should be synced, got %v", d["name"])
	}

	// Empty name -> 400.
	c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d", flowID), `{"name":"  "}`, http.StatusBadRequest)
	c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d", flowID), `{"name":""}`, http.StatusBadRequest)

	// Read-only member cannot rename.
	c2 := &client{t: t, ts: c.ts}
	readerID := c2.registerAndLoginAs("renamereader")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/members", testSetID),
		fmt.Sprintf(`{"user_id":%d,"role":"read"}`, readerID), http.StatusCreated)
	c2.do("PATCH", fmt.Sprintf("/api/flow/flows/%d", flowID), `{"name":"hack"}`, http.StatusForbidden)
}

func TestFlowThinkingPersists(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()

	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"flow t"}`, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	_, draft := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if draft["thinking"] != "disabled" && draft["thinking"] != "" {
		t.Fatalf("default thinking unexpected: %v", draft["thinking"])
	}

	// 通过专用端点保存思考级别。
	_, got := c.do("PATCH", fmt.Sprintf("/api/flow/flows/%d/thinking", flowID), `{"thinking":"low"}`, http.StatusOK)
	if got["thinking"] != "low" {
		t.Fatalf("thinking PATCH should return low, got %v", got["thinking"])
	}

	// GET 草稿应能读回。
	_, draft2 := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if draft2["thinking"] != "low" {
		t.Fatalf("draft GET should reflect low, got %v", draft2["thinking"])
	}
}
