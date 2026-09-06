package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

func createTestSetAndDoc(t *testing.T, c *client) (uint, uint) {
	t.Helper()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"case-set"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, doc := c.do("POST", fmt.Sprintf("/api/test-sets/%d/background-documents", testSetID), `{"name":"需求","content":"登录需求"}`, http.StatusCreated)
	return testSetID, uint(doc["id"].(float64))
}

func TestBackgroundDocumentAPIAndPermission(t *testing.T) {
	c := newClient(t)
	uid := c.registerAndLogin()
	testSetID, docID := createTestSetAndDoc(t, c)
	if docID == 0 {
		t.Fatal("doc id missing")
	}
	_, list := c.do("GET", fmt.Sprintf("/api/test-sets/%d/background-documents", testSetID), "", http.StatusOK)
	if len(list["documents"].([]any)) != 1 {
		t.Fatal("expected one document")
	}
	_, _ = c.do("PATCH", fmt.Sprintf("/api/test-sets/%d/background-documents/%d", testSetID, docID), `{"name":"改"}`, http.StatusOK)
	_ = uid

	// 只读成员无法编辑
	c.registerAndLoginAs("bob")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/background-documents", testSetID), `{"name":"x","content":"y"}`, http.StatusForbidden)
	c.do("PATCH", fmt.Sprintf("/api/test-sets/%d/background-documents/%d", testSetID, docID), `{"name":"x"}`, http.StatusForbidden)
}

func TestCaseFlowLifecycleAndVersionRestore(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	testSetID, docID := createTestSetAndDoc(t, c)

	// 拒绝空来源
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID), `{"name":"空","sources":[]}`, http.StatusBadRequest)

	_, cf := c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID), fmt.Sprintf(`{"name":"用例流","sources":[{"kind":"document","document_id":%d}]}`, docID), http.StatusCreated)
	caseFlowID := uint(cf["id"].(float64))

	_, view := c.do("GET", fmt.Sprintf("/api/case-flows/%d/draft", caseFlowID), "", http.StatusOK)
	rev := uint(view["draft"].(map[string]any)["revision"].(float64))
	rootID := view["tree"].(map[string]any)["root"].(map[string]any)["id"].(string)

	_, draft := c.do("POST", fmt.Sprintf("/api/case-flows/%d/nodes", caseFlowID), fmt.Sprintf(`{"revision":%d,"parent_id":"%s","title":"登录用例"}`, rev, rootID), http.StatusCreated)
	_ = draft

	_, ver := c.do("POST", fmt.Sprintf("/api/case-flows/%d/versions", caseFlowID), "", http.StatusCreated)
	if int(ver["version_no"].(float64)) != 1 {
		t.Fatal("expected version 1")
	}

	_, verList := c.do("GET", fmt.Sprintf("/api/case-flows/%d/versions", caseFlowID), "", http.StatusOK)
	if len(verList["versions"].([]any)) != 1 {
		t.Fatal("expected one version")
	}

	c.do("POST", fmt.Sprintf("/api/case-flows/%d/versions/1/restore", caseFlowID), "", http.StatusOK)
}

func TestCaseNodeStatusCascadeAndFlows(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	testSetID, docID := createTestSetAndDoc(t, c)
	_, cf := c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID), fmt.Sprintf(`{"name":"状态流","sources":[{"kind":"document","document_id":%d}]}`, docID), http.StatusCreated)
	caseFlowID := uint(cf["id"].(float64))
	_, view := c.do("GET", fmt.Sprintf("/api/case-flows/%d/draft", caseFlowID), "", http.StatusOK)
	rev := uint(view["draft"].(map[string]any)["revision"].(float64))
	rootID := view["tree"].(map[string]any)["root"].(map[string]any)["id"].(string)
	_, _ = c.do("POST", fmt.Sprintf("/api/case-flows/%d/nodes", caseFlowID), fmt.Sprintf(`{"revision":%d,"parent_id":"%s","title":"子"}`, rev, rootID), http.StatusCreated)

	// 根状态级联
	_, _ = c.do("PATCH", fmt.Sprintf("/api/case-flows/%d/nodes/%s/status", caseFlowID, rootID), fmt.Sprintf(`{"revision":%d,"status":"covered"}`, rev+1), http.StatusOK)
	// 关联执行流列表查询
	_, _ = c.do("GET", fmt.Sprintf("/api/case-flows/%d/nodes/%s/flows", caseFlowID, rootID), "", http.StatusOK)
}
