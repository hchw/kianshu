package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// caseFlowRoot reads a case flow draft and returns its root id and revision.
func caseFlowRoot(t *testing.T, c *client, caseFlowID uint) (string, int) {
	t.Helper()
	_, draft := c.do("GET", fmt.Sprintf("/api/case-flows/%d/draft", caseFlowID), "", http.StatusOK)
	tree, _ := draft["tree"].(map[string]any)
	root, _ := tree["root"].(map[string]any)
	rootID, _ := root["id"].(string)
	d, _ := draft["draft"].(map[string]any)
	rev, _ := d["revision"].(float64)
	if rootID == "" {
		t.Fatalf("case flow %d has no root", caseFlowID)
	}
	return rootID, int(rev)
}

// TestCreateFlowFromCasesBindsVersion covers task 2.3: a bound creation path
// that records the case binding, the unchanged blank path, rejection of a
// draft-only case flow, and the read-only member rejection.
func TestCreateFlowFromCasesBindsVersion(t *testing.T) {
	c := newClient(t)
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))

	_, doc := c.do("POST", fmt.Sprintf("/api/test-sets/%d/background-documents", testSetID), `{"name":"需求文档","content":"登录后下单"}`, http.StatusCreated)
	docID := uint(doc["id"].(float64))
	_, cf := c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID), fmt.Sprintf(`{"name":"用例流","sources":[{"kind":"document","document_id":%d}]}`, docID), http.StatusCreated)
	caseFlowID := uint(cf["id"].(float64))

	rootID, rev := caseFlowRoot(t, c, caseFlowID)
	c.do("POST", fmt.Sprintf("/api/case-flows/%d/nodes", caseFlowID), fmt.Sprintf(`{"revision":%d,"parent_id":"%s","title":"正常登录"}`, rev, rootID), http.StatusCreated)
	rootID, rev = caseFlowRoot(t, c, caseFlowID)
	c.do("POST", fmt.Sprintf("/api/case-flows/%d/nodes", caseFlowID), fmt.Sprintf(`{"revision":%d,"parent_id":"%s","title":"异常密码"}`, rev, rootID), http.StatusCreated)
	c.do("POST", fmt.Sprintf("/api/case-flows/%d/versions", caseFlowID), "", http.StatusCreated)

	boundBody := fmt.Sprintf(`{"name":"执行流","case_selections":[{"case_flow_id":%d,"root_ids":["%s"]}]}`, caseFlowID, rootID)
	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), boundBody, http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	_, draft := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	binding, _ := draft["case_binding"].(string)
	if !strings.Contains(binding, "case_flow_id") || !strings.Contains(binding, "正常登录") || !strings.Contains(binding, "异常密码") {
		t.Fatalf("draft binding = %q", binding)
	}

	// Blank creation is unchanged.
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"空白流"}`, http.StatusCreated)

	// A case flow with no saved version must be rejected.
	_, cf2 := c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID), fmt.Sprintf(`{"name":"无版本","sources":[{"kind":"document","document_id":%d}]}`, docID), http.StatusCreated)
	cf2ID := uint(cf2["id"].(float64))
	root2, _ := caseFlowRoot(t, c, cf2ID)
	badBody := fmt.Sprintf(`{"name":"坏流","case_selections":[{"case_flow_id":%d,"root_ids":["%s"]}]}`, cf2ID, root2)
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), badBody, http.StatusBadRequest)

	// Read-only member is rejected.
	bob := &client{t: t, ts: c.ts}
	bobID := bob.registerAndLoginAs("bob")
	c.do("POST", fmt.Sprintf("/api/test-sets/%d/members", testSetID), fmt.Sprintf(`{"user_id":%d,"role":"read"}`, bobID), http.StatusCreated)
	bob.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), boundBody, http.StatusForbidden)
	// Read-only cannot generate or save, but can read the flow's case sources.
	bob.do("POST", fmt.Sprintf("/api/flow/flows/%d/generate-from-cases", flowID), `{"provider_id":1}`, http.StatusForbidden)
	bob.do("POST", fmt.Sprintf("/api/flow/flows/%d/save-from-cases", flowID), "", http.StatusForbidden)
	bob.do("GET", fmt.Sprintf("/api/flow/flows/%d/case-sources", flowID), "", http.StatusOK)
}

// TestAgentGenerateUsesCaseBinding covers the unified agent entry point: for a
// flow bound to a case flow, `mode=generate` must be case-driven (pre-building
// case-unit anchors) and must accept an empty instruction; a blank flow keeps
// requiring an instruction.
func TestAgentGenerateUsesCaseBinding(t *testing.T) {
	fake := &agentFakeProvider{model: "m", script: []agentRound{{content: strPtrA("完成")}}}
	c := &client{t: t, ts: agentSharedServer(t, fake)}
	c.registerAndLogin()
	_, ts := c.do("POST", "/api/test-sets", `{"name":"demo"}`, http.StatusCreated)
	testSetID := uint(ts["id"].(float64))
	_, prov := c.do("POST", "/api/providers",
		`{"name":"llm","base_url":"http://fake","api_key":"sk","model":"m","enabled":true}`, http.StatusCreated)
	providerID := uint(prov["id"].(float64))

	_, doc := c.do("POST", fmt.Sprintf("/api/test-sets/%d/background-documents", testSetID),
		`{"name":"需求文档","content":"登录"}`, http.StatusCreated)
	docID := uint(doc["id"].(float64))
	_, cf := c.do("POST", fmt.Sprintf("/api/test-sets/%d/case-flows", testSetID),
		fmt.Sprintf(`{"name":"用例流","sources":[{"kind":"document","document_id":%d}]}`, docID), http.StatusCreated)
	caseFlowID := uint(cf["id"].(float64))
	rootID, rev := caseFlowRoot(t, c, caseFlowID)
	c.do("POST", fmt.Sprintf("/api/case-flows/%d/nodes", caseFlowID),
		fmt.Sprintf(`{"revision":%d,"parent_id":"%s","title":"正常登录"}`, rev, rootID), http.StatusCreated)
	c.do("POST", fmt.Sprintf("/api/case-flows/%d/versions", caseFlowID), "", http.StatusCreated)

	_, fl := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID),
		fmt.Sprintf(`{"name":"执行流","case_selections":[{"case_flow_id":%d,"root_ids":["%s"]}]}`, caseFlowID, rootID),
		http.StatusCreated)
	flowID := uint(fl["id"].(float64))

	// 绑定流：generate 无需 instruction，按用例骨架生成。
	body := fmt.Sprintf(`{"provider_id":%d,"mode":"generate"}`, providerID)
	_, out := c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", flowID), body, http.StatusOK)
	if out["finished"] != true {
		t.Fatalf("expected finished, got %v", out)
	}
	_, draft := c.do("GET", fmt.Sprintf("/api/flow/flows/%d/draft", flowID), "", http.StatusOK)
	if !strings.Contains(draft["tree"].(string), `"type":"case-unit"`) {
		t.Fatalf("draft should carry case-unit anchors, got %v", draft["tree"])
	}

	// 空白流：generate 仍要求 instruction。
	_, blank := c.do("POST", fmt.Sprintf("/api/test-sets/%d/flows", testSetID), `{"name":"空白流"}`, http.StatusCreated)
	blankID := uint(blank["id"].(float64))
	c.do("POST", fmt.Sprintf("/api/flow/flows/%d/agent/submit", blankID),
		fmt.Sprintf(`{"provider_id":%d,"mode":"generate"}`, providerID), http.StatusBadRequest)
}
