package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github/hchw/kianshu/internal/flow"
)

// nodeCfg marshals a config map onto a node.
func nodeCfg(t *testing.T, n *flow.Node, v any) {
	t.Helper()
	cfg, err := flow.MarshalConfig(v)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	n.Config = cfg
}

// treeOf builds a tree from a start id and nodes.
func treeOf(nodes map[string]*flow.Node) *flow.Tree {
	return &flow.Tree{Start: "n1", Nodes: nodes}
}

func TestLinearChain(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAdapter),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"expr": `$.n * 2`})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$ + 1`})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")

	res, err := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(2)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %s, want ok", res.Status)
	}
	out := res.Results["n3"].Output
	if !reflect.DeepEqual(out, float64(5)) {
		t.Fatalf("final output = %v, want 5", out)
	}
}

func TestSiblingBranchesBothRun(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAdapter),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"expr": `$.n * 10`})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$.n * 100`})
	tree.AddChild("n1", "n2")
	tree.AddChild("n1", "n3")

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(1)}})
	if res.Results["n2"].Output != float64(10) {
		t.Fatalf("n2 output = %v", res.Results["n2"].Output)
	}
	if res.Results["n3"].Output != float64(100) {
		t.Fatalf("n3 output = %v", res.Results["n3"].Output)
	}
}

func TestFailureStopsOnlyFailingSubtree(t *testing.T) {
	// n1 -> n2 (fails via bad adapter) and n1 -> n3 (sibling, runs)
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAdapter),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"expr": `(`}) // invalid JSONata
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$.n * 7`})
	tree.AddChild("n1", "n2")
	tree.AddChild("n1", "n3")

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(0)}})
	if res.Results["n2"].Status != StatusFailed {
		t.Fatalf("n2 status = %s, want failed", res.Results["n2"].Status)
	}
	if res.Results["n3"].Status != StatusOK || res.Results["n3"].Output != float64(0) {
		t.Fatalf("n3 should still run, got %+v", res.Results["n3"])
	}
	if res.Status != StatusFailed {
		t.Fatalf("run status = %s, want failed", res.Status)
	}
}

func TestTryWithoutCatchSoftStops(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeTry),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
		"n4": flow.NewNode("n4", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `(`}) // fails
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `$.n * 3`})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n1", "n4") // sibling of try, still runs

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(2)}})
	if res.Results["n2"].Status != StatusSoftStop {
		t.Fatalf("try status = %s, want soft-stop", res.Results["n2"].Status)
	}
	if res.Results["n4"].Status != StatusOK {
		t.Fatalf("try sibling should still run, got %+v", res.Results["n4"])
	}
}

func TestCatchFallbackAndPassThrough(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeTry),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
		"n4": flow.NewNode("n4", flow.NodeCatch),
	})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$.n * 2`})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"fallback": "fb"})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n2", "n4")

	// Success path: catch passes the preceding output through.
	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(3)}})
	if res.Results["n2"].Status != StatusOK {
		t.Fatalf("try status = %s", res.Results["n2"].Status)
	}
	if res.Results["n4"].Status != StatusOK {
		t.Fatalf("catch status = %s", res.Results["n4"].Status)
	}

	// Error path: adapter fails, catch outputs fallback, try stays ok.
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `(`})
	res2, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(3)}})
	if res2.Results["n2"].Status != StatusOK {
		t.Fatalf("try with catch should contain failure, got %s", res2.Results["n2"].Status)
	}
	if res2.Results["n4"].Status != StatusOK {
		t.Fatalf("catch status = %s", res2.Results["n4"].Status)
	}
}

func TestInnerCatchContainsError(t *testing.T) {
	// outer try -> inner try(with catch) -> failing adapter
	// The inner catch handles it; outer try must not soft-stop.
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeTry), // outer
		"n3": flow.NewNode("n3", flow.NodeTry), // inner
		"n4": flow.NewNode("n4", flow.NodeAdapter),
		"n5": flow.NewNode("n5", flow.NodeCatch),
		"n6": flow.NewNode("n6", flow.NodeCatch),
	})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `(`})
	nodeCfg(t, tree.Nodes["n5"], map[string]any{"fallback": "inner-fb"})
	nodeCfg(t, tree.Nodes["n6"], map[string]any{"fallback": "outer-fb"})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n3", "n4")
	tree.AddChild("n3", "n5") // inner catch
	tree.AddChild("n2", "n6") // outer catch

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{}})
	if res.Results["n3"].Status != StatusOK {
		t.Fatalf("inner try = %s, want ok (caught)", res.Results["n3"].Status)
	}
	if res.Results["n2"].Status != StatusOK {
		t.Fatalf("outer try should not trigger, got %s", res.Results["n2"].Status)
	}
}

func TestInnerTryWithoutCatchSoftStops(t *testing.T) {
	// outer try -> inner try(no catch) -> failing adapter; outer try has catch.
	// Inner soft-stops, outer catch must NOT handle it.
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeTry), // outer, has catch
		"n3": flow.NewNode("n3", flow.NodeTry), // inner, no catch
		"n4": flow.NewNode("n4", flow.NodeAdapter),
		"n5": flow.NewNode("n5", flow.NodeCatch),
	})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `(`})
	nodeCfg(t, tree.Nodes["n5"], map[string]any{"fallback": "outer-fb"})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n3", "n4")
	tree.AddChild("n2", "n5")

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{}})
	if res.Results["n3"].Status != StatusSoftStop {
		t.Fatalf("inner try = %s, want soft-stop", res.Results["n3"].Status)
	}
	if res.Results["n2"].Status != StatusSoftStop {
		t.Fatalf("outer try should soft-stop (inner soft-stop must not bubble into catch), got %s", res.Results["n2"].Status)
	}
}

func TestLoopSuccessAndFailure(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeLoop),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"input": "items", "var": "item"})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `item * 2`})
	// loop declares its input key
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{"items": {Type: flow.IOTypeArray}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")

	start := map[string]any{"items": []any{float64(1), float64(2), float64(3)}}
	res, _ := Run(context.Background(), tree, Options{StartParams: start})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	want := []any{float64(2), float64(4), float64(6)}
	if !reflect.DeepEqual(res.Results["n2"].Output, want) {
		t.Fatalf("loop output = %v, want %v", res.Results["n2"].Output, want)
	}

	// Failed iteration soft-stops the loop.
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `item / 0`})
	res2, _ := Run(context.Background(), tree, Options{StartParams: start})
	if res2.Results["n2"].Status != StatusSoftStop {
		t.Fatalf("loop with failed iteration = %s, want soft-stop", res2.Results["n2"].Status)
	}
	if res2.Results["n2"].Output != nil {
		t.Fatalf("soft-stopped loop should have no output, got %v", res2.Results["n2"].Output)
	}
}

func TestCatchRunsOnceOnFallback(t *testing.T) {
	// try -> branch (adapter) with children [failing api, branch-local catch].
	// runNode walks the whole failing subtree (api fails, catch pass-throughs,
	// catch child runs), then runTry re-handles via findCatchInSubtree +
	// execCatchFailure. The catch must not re-execute its already-run subtree:
	// api runs once, catch child runs once, catch outputs fallback once.
	var apiCalls int
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeTry),
		"n3": flow.NewNode("n3", flow.NodeAdapter), // branch root
		"n4": flow.NewNode("n4", flow.NodeAPI),     // protected, fails
		"n5": flow.NewNode("n5", flow.NodeCatch),   // branch-local catch
		"n6": flow.NewNode("n6", flow.NodeAPI),     // catch child (side effect)
		"n7": flow.NewNode("n7", flow.NodeAdapter), // sibling of try
	})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$`})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"unit": map[string]any{"method": "GET", "path": "/boom"}})
	nodeCfg(t, tree.Nodes["n5"], map[string]any{"fallback": "fb"})
	nodeCfg(t, tree.Nodes["n6"], map[string]any{"unit": map[string]any{"method": "GET", "path": "/ok"}})
	nodeCfg(t, tree.Nodes["n7"], map[string]any{"expr": `$.ok`})
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n3", "n4")
	tree.AddChild("n3", "n5") // branch-local catch
	tree.AddChild("n5", "n6") // catch child
	tree.AddChild("n1", "n7") // sibling of try

	res, _ := Run(context.Background(), tree, Options{
		Host:        "https://api.example.com",
		StartParams: map[string]any{"ok": "sibling"},
		CallAPI: func(c APICall) (*APIResponse, error) {
			apiCalls++
			if strings.HasSuffix(c.URL, "/ok") {
				return &APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
			}
			return nil, fmt.Errorf("http error")
		},
	})
	if apiCalls != 2 {
		t.Fatalf("api called %d times, want 2 (failing api once + catch child once)", apiCalls)
	}
	if res.Results["n4"].Status != StatusFailed {
		t.Fatalf("api status = %s, want failed", res.Results["n4"].Status)
	}
	if res.Results["n5"].Output != "fb" {
		t.Fatalf("catch output = %v, want fallback fb", res.Results["n5"].Output)
	}
	if res.Results["n6"].Status != StatusOK {
		t.Fatalf("catch child should have run, got %+v", res.Results["n6"])
	}
	if res.Results["n2"].Status != StatusOK {
		t.Fatalf("try status = %s, want ok (failure contained)", res.Results["n2"].Status)
	}
	if res.Results["n7"].Status != StatusOK {
		t.Fatalf("try sibling should still run, got %+v", res.Results["n7"])
	}
}

func TestCacheWriteAndRead(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
		"n3": flow.NewNode("n3", flow.NodeCacheSet),
		"n4": flow.NewNode("n4", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"unit": map[string]any{"method": "POST", "path": "/login"},
	})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"writes": map[string]any{"token": "body.resp.token"}})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `$.token`})
	tree.Nodes["n4"].Inputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive, Source: "$cache.token"}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n2", "n4") // reader 是兄弟,非子节点,先写后读

	res, _ := Run(context.Background(), tree, Options{
		Host: "https://api.example.com",
		CallAPI: func(c APICall) (*APIResponse, error) {
			return &APIResponse{StatusCode: 200, Body: map[string]any{"resp": map[string]any{"token": "abc123"}}}, nil
		},
	})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	if res.Cache["token"] == nil {
		t.Fatalf("cache token not written: %v", res.Cache)
	}
	if res.Results["n4"].Output != "abc123" {
		t.Fatalf("cache read output = %v, want abc123", res.Results["n4"].Output)
	}
}

func TestCacheIsolationBetweenRuns(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeCacheSet),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"writes": map[string]any{"k": `$.v`}})
	tree.AddChild("n1", "n2")

	// Run 1 writes k=run1 to its run-scoped cache.
	res1, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"v": "run1"}})
	if res1.Cache["k"] != "run1" {
		t.Fatalf("run1 cache = %v, want k=run1", res1.Cache)
	}
	// Run 2 starts with a fresh cache: it must not see run1's value.
	res2, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{"v": "run2"}})
	if res2.Cache["k"] != "run2" {
		t.Fatalf("cache not isolated between runs: %v, want k=run2", res2.Cache)
	}
}

func TestAssertNode(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAssert),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status", "op": "eq", "expected": float64(200)},
		},
	})
	tree.AddChild("n1", "n2")
	start := map[string]any{"status": float64(200)}
	res, _ := Run(context.Background(), tree, Options{StartParams: start})
	if res.Results["n2"].Status != StatusOK {
		t.Fatalf("assert pass = %s", res.Results["n2"].Status)
	}

	start2 := map[string]any{"status": float64(500)}
	res2, _ := Run(context.Background(), tree, Options{StartParams: start2})
	if res2.Results["n2"].Status != StatusFailed {
		t.Fatalf("assert fail = %s, want failed", res2.Results["n2"].Status)
	}
}

func TestAPINodeUsesHostAndSnapshot(t *testing.T) {
	var got APICall
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"unit": map[string]any{"method": "POST", "path": "/login"}})
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"account":       {Type: flow.IOTypePrimitive},
		"authorization": {Type: flow.IOTypePrimitive},
	}
	tree.AddChild("n1", "n2")

	res, _ := Run(context.Background(), tree, Options{
		Host: "https://api.example.com/",
		StartParams: map[string]any{
			"account":       "alice",
			"authorization": "Bearer t",
		},
		CallAPI: func(c APICall) (*APIResponse, error) {
			got = c
			return &APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
		},
	})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	if got.URL != "https://api.example.com/login" {
		t.Fatalf("url = %s", got.URL)
	}
	if got.Method != "POST" {
		t.Fatalf("method = %s", got.Method)
	}
	if got.Headers["authorization"] != "Bearer t" {
		t.Fatalf("auth header = %v", got.Headers)
	}
	if got.Body.(map[string]any)["account"] != "alice" {
		t.Fatalf("body = %v", got.Body)
	}
}

func TestAPINodeParamsAndPathSubstitution(t *testing.T) {
	var got APICall
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"unit": map[string]any{"method": "GET", "path": "/users/{id}/orders/{status}"},
		// Explicit params: literal override, path placeholder, JSONata expr.
		"params": map[string]any{
			"id":     float64(42),
			"status": "open",
			"page":   "=2 + 1",
			"scope":  "all",
		},
	})
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"id":    {Type: flow.IOTypePrimitive},
		"scope": {Type: flow.IOTypePrimitive},
	}
	tree.AddChild("n1", "n2")

	res, _ := Run(context.Background(), tree, Options{
		Host:        "https://api.example.com",
		StartParams: map[string]any{"id": "from-start", "scope": "from-upstream", "unused": "x"},
		CallAPI: func(c APICall) (*APIResponse, error) {
			got = c
			return &APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
		},
	})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	if got.URL != "https://api.example.com/users/42/orders/open" {
		t.Fatalf("url = %s, want path placeholders substituted", got.URL)
	}
	// JSONata-evaluated param becomes a query value.
	if got.Query["page"] != "3" && got.Query["page"] != float64(3) {
		t.Fatalf("query page = %v, want evaluated 3", got.Query["page"])
	}
	// Explicit param overrides upstream output with the same key.
	if got.Query["scope"] != "all" {
		t.Fatalf("query scope = %v, want explicit param override", got.Query["scope"])
	}
	// Path placeholders are removed from the query/body routing.
	if _, has := got.Query["id"]; has {
		t.Fatalf("path placeholder id should not be routed as query: %v", got.Query)
	}
	if _, has := got.Query["status"]; has {
		t.Fatalf("path placeholder status should not be routed as query: %v", got.Query)
	}
}

func TestCacheSetOutputsFullCache(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeCacheSet),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"writes": map[string]any{"a": `"x"`, "b": `"y"`}})
	tree.AddChild("n1", "n2")

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{}})
	out, _ := json.Marshal(res.Results["n2"].Output)
	if string(out) != `{"a":"x","b":"y"}` {
		t.Fatalf("cache-set output = %s", out)
	}
}

func TestCacheSetLiteralValues(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeCacheSet),
	})
	// 非 string 类型直接当字面量写入
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"writes": map[string]any{
			"count": float64(3),
			"flag":  true,
			"items": []any{"a", "b"},
		},
	})
	tree.AddChild("n1", "n2")

	res, _ := Run(context.Background(), tree, Options{StartParams: map[string]any{}})

	// 数字
	if res.Cache["count"] != float64(3) {
		t.Fatalf("cache[count] = %v, want 3", res.Cache["count"])
	}
	// 布尔
	if res.Cache["flag"] != true {
		t.Fatalf("cache[flag] = %v, want true", res.Cache["flag"])
	}
	// 数组
	items, ok := res.Cache["items"].([]any)
	if !ok || len(items) != 2 || items[0] != "a" {
		t.Fatalf("cache[items] = %v, want [a b]", res.Cache["items"])
	}
}

func TestCacheSetStaticVars(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeCacheSet),
	})
	// $static.invalid 引用 static 里的固定字符串
	// StartParams 模拟 api 的信封输出 {body: {token: "abc123"}}
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"writes": map[string]any{
			"token":        "body.token",
			"invalid_auth": "$static.invalid",
		},
		"static": map[string]interface{}{
			"invalid": "fake_invalid_token_12345",
		},
	})
	tree.AddChild("n1", "n2")

	res, err := Run(context.Background(), tree, Options{
		StartParams: map[string]any{"body": map[string]any{"token": "abc123"}},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusOK {
		for id, r := range res.Results {
			t.Logf("node %s: status=%s err=%v", id, r.Status, r.Error)
		}
		t.Fatalf("status = %s", res.Status)
	}
	if res.Cache["token"] != "abc123" {
		t.Fatalf("cache[token] = %v, want abc123", res.Cache["token"])
	}
	if res.Cache["invalid_auth"] != "fake_invalid_token_12345" {
		t.Fatalf("cache[invalid_auth] = %v, want fake_invalid_token_12345", res.Cache["invalid_auth"])
	}
}

func TestRunContextTimeoutFailsNodes(t *testing.T) {
	// An already-expired context must fail every node with a timeout error
	// instead of executing it, even without any HTTP call involved.
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"expr": `$`})
	tree.AddChild("n1", "n2")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ := Run(ctx, tree, Options{StartParams: map[string]any{"x": 1}})
	if res.Status != StatusFailed {
		t.Fatalf("run status = %s, want failed", res.Status)
	}
	if res.Results["n2"].Status != StatusFailed {
		t.Fatalf("node status = %s, want failed on expired context", res.Results["n2"].Status)
	}
	if !strings.Contains(res.Results["n2"].Error, "执行超时") {
		t.Fatalf("error = %q, want timeout message", res.Results["n2"].Error)
	}
}

func TestLoopAggregatesMultipleChildren(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeLoop),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
		"n4": flow.NewNode("n4", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"input": "items", "var": "item"})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `item * 2`})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `item + 100`})
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{"items": {Type: flow.IOTypeArray}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n2", "n4")

	start := map[string]any{"items": []any{float64(1), float64(2)}}
	res, _ := Run(context.Background(), tree, Options{StartParams: start})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	out, ok := res.Results["n2"].Output.([]any)
	if !ok || len(out) != 2 {
		t.Fatalf("loop output = %v, want 2 iterations", res.Results["n2"].Output)
	}
	first, ok := out[0].(map[string]any)
	if !ok || first["n3"] != float64(2) || first["n4"] != float64(101) {
		t.Fatalf("first iteration output = %v, want aggregated map", out[0])
	}
}

func TestCacheSetProducerPush(t *testing.T) {
	// cache-set runs as a tree node, writing to the shared cache before
	// downstream nodes read from it.
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeCacheSet),
		"n3": flow.NewNode("n3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"writes": map[string]any{"token": `$.resp`}})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"expr": `$.token`})
	tree.Nodes["n3"].Inputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n1", "n3") // reader 是兄弟,先写后读

	res, _ := Run(context.Background(), tree, Options{
		StartParams: map[string]any{"resp": "bare-token"},
	})
	if res.Status != StatusOK {
		t.Fatalf("run status = %s", res.Status)
	}
	if res.Cache["token"] != "bare-token" {
		t.Fatalf("cache token = %v, want bare-token", res.Cache["token"])
	}
	if res.Results["n3"].Output != "bare-token" {
		t.Fatalf("node output = %v, want bare-token", res.Results["n3"].Output)
	}
	if res.Results["n2"].Status != StatusOK {
		t.Fatalf("cache-set should have run as tree node, got %+v", res.Results["n2"])
	}
}

func TestAPIAuthHeaderFromSecurityScheme(t *testing.T) {
	cases := []struct {
		name     string
		security string
		key      string
		value    string
		wantKey  string
		wantVal  string
	}{
		{"bearer 裸 token 加前缀", `[{"BearerAuth":[]}]`, "auth", "abc123", "Authorization", "Bearer abc123"},
		{"bearer 已带前缀不重复", `[{"BearerAuth":[]}]`, "auth", "Bearer abc123", "Authorization", "Bearer abc123"},
		{"apikey 走 X-API-Key", `[{"apikey":[]}]`, "api_key", "k-9", "X-API-Key", "k-9"},
		{"无声明保持原键原值", "", "authorization", "raw", "authorization", "raw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got APICall
			tree := treeOf(map[string]*flow.Node{
				"n1": flow.NewNode("n1", flow.NodeStart),
				"n2": flow.NewNode("n2", flow.NodeAPI),
			})
			unit := map[string]any{"method": "GET", "path": "/x"}
			if c.security != "" {
				unit["security"] = c.security
			}
			nodeCfg(t, tree.Nodes["n2"], map[string]any{"unit": unit})
			tree.Nodes["n2"].Inputs = map[string]flow.IOKey{c.key: {Type: flow.IOTypePrimitive}}
			tree.AddChild("n1", "n2")
			res, err := Run(context.Background(), tree, Options{
				Host:        "http://x",
				StartParams: map[string]any{c.key: c.value},
				CallAPI: func(call APICall) (*APIResponse, error) {
					got = call
					return &APIResponse{StatusCode: 200, Body: map[string]any{}}, nil
				},
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.Status != StatusOK {
				t.Fatalf("status = %s", res.Status)
			}
			if got.Headers[c.wantKey] != c.wantVal {
				t.Fatalf("header = %v, want %s=%s", got.Headers, c.wantKey, c.wantVal)
			}
		})
	}
}

// TestAPICacheChainAncestorLayout 祖先结构链路:login 输出 token → cache-set
// 写入共享缓存(挂在 login 下)→ reader 挂在 cache-set 下读取 $cache.token,
// 执行顺序由"父先于子"保证,无需同级排序。
func TestAPICacheChainAncestorLayout(t *testing.T) {
	var readerCall APICall
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
		"cs": flow.NewNode("cs", flow.NodeCacheSet),
		"n3": flow.NewNode("n3", flow.NodeAPI),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"unit": map[string]any{"method": "POST", "path": "/login"}})
	nodeCfg(t, tree.Nodes["cs"], map[string]any{"writes": map[string]string{"token": "body.token"}})
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"unit": map[string]any{
		"method": "GET", "path": "/test-sets", "security": `[{"BearerAuth":[]}]`,
	}})
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"username": {Type: flow.IOTypePrimitive, Source: "username"},
		"password": {Type: flow.IOTypePrimitive, Source: "password"},
	}
	tree.Nodes["n3"].Inputs = map[string]flow.IOKey{"auth": {Type: flow.IOTypePrimitive, Source: "$cache.token"}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "cs")
	tree.AddChild("cs", "n3")

	calls := 0
	res, err := Run(context.Background(), tree, Options{
		Host:        "http://x",
		StartParams: map[string]any{"username": "u", "password": "p"},
		CallAPI: func(c APICall) (*APIResponse, error) {
			calls++
			if calls == 1 {
				return &APIResponse{StatusCode: 200, Body: map[string]any{"token": "abc123"}}, nil
			}
			readerCall = c
			return &APIResponse{StatusCode: 200, Body: map[string]any{}}, nil
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %s, results: %v", res.Status, res.Results)
	}
	if readerCall.Headers["Authorization"] != "Bearer abc123" {
		t.Fatalf("reader auth header = %v, want Authorization: Bearer abc123", readerCall.Headers)
	}
	if calls != 2 {
		t.Fatalf("expected 2 api calls, got %d", calls)
	}
}

// TestFlow13ExecutionOrder reproduces the flow-13 tree and verifies the
// execution order is strict depth-first. Every node records a sequence number
// so the test can assert the expected DFS ordering.
func TestFlow13ExecutionOrder(t *testing.T) {
	// Reconstruct the tree from flow 13 (run id=46).
	tree := &flow.Tree{Start: "n1", Nodes: map[string]*flow.Node{
		"n1":                     {ID: "n1", Type: flow.NodeStart},
		"n_seed":                 {ID: "n_seed", Type: flow.NodeCacheSet},
		"n_login":                {ID: "n_login", Type: flow.NodeAPI},
		"n_cache_token":          {ID: "n_cache_token", Type: flow.NodeCacheSet},
		"n_token_valid":          {ID: "n_token_valid", Type: flow.NodeAPI},
		"n_assert_token_valid":   {ID: "n_assert_token_valid", Type: flow.NodeAssert},
		"n_logout":               {ID: "n_logout", Type: flow.NodeAPI},
		"n_token_invalid":        {ID: "n_token_invalid", Type: flow.NodeAPI},
		"n_assert_token_invalid": {ID: "n_assert_token_invalid", Type: flow.NodeAssert},
		"n_assert_logout":        {ID: "n_assert_logout", Type: flow.NodeAssert},
		"n_assert_login":         {ID: "n_assert_login", Type: flow.NodeAssert},
		"n_try_register":         {ID: "n_try_register", Type: flow.NodeTry},
		"n_register":             {ID: "n_register", Type: flow.NodeAPI},
		"n_assert_register":      {ID: "n_assert_register", Type: flow.NodeAssert},
		"n_catch_register":       {ID: "n_catch_register", Type: flow.NodeCatch},
	}}

	// Link: n1 -> n_seed
	tree.AddChild("n1", "n_seed")
	// n_seed -> n_login, n_try_register
	tree.AddChild("n_seed", "n_login")
	tree.AddChild("n_seed", "n_try_register")
	// n_login -> n_cache_token, n_assert_login
	tree.AddChild("n_login", "n_cache_token")
	tree.AddChild("n_login", "n_assert_login")
	// n_cache_token -> n_token_valid
	tree.AddChild("n_cache_token", "n_token_valid")
	// n_token_valid -> n_assert_token_valid, n_logout
	tree.AddChild("n_token_valid", "n_assert_token_valid")
	tree.AddChild("n_token_valid", "n_logout")
	// n_logout -> n_token_invalid, n_assert_logout
	tree.AddChild("n_logout", "n_token_invalid")
	tree.AddChild("n_logout", "n_assert_logout")
	// n_token_invalid -> n_assert_token_invalid
	tree.AddChild("n_token_invalid", "n_assert_token_invalid")
	// n_try_register -> n_register
	tree.AddChild("n_try_register", "n_register")
	// n_register -> n_assert_register, n_catch_register
	tree.AddChild("n_register", "n_assert_register")
	tree.AddChild("n_register", "n_catch_register")

	// Config: n_seed writes username + password to cache
	nodeCfg(t, tree.Nodes["n_seed"], map[string]any{
		"writes": map[string]string{
			"username": "'testuser2'",
			"password": "'test123'",
		},
	})

	// Config: n_cache_token writes token to cache (envelope: body.token)
	nodeCfg(t, tree.Nodes["n_cache_token"], map[string]any{
		"writes": map[string]string{"token": "body.token"},
	})

	// Config: n_try_register (try node, no special config needed)
	nodeCfg(t, tree.Nodes["n_try_register"], map[string]any{})

	// Config: n_catch_register catch node with fallback
	nodeCfg(t, tree.Nodes["n_catch_register"], map[string]any{
		"fallback": map[string]any{"registered": false, "reason": "username exists"},
	})

	// Config: n_assert_login
	nodeCfg(t, tree.Nodes["n_assert_login"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status_code", "op": "eq", "expected": float64(200)},
		},
	})
	nodeCfg(t, tree.Nodes["n_assert_logout"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status_code", "op": "eq", "expected": float64(200)},
		},
	})
	nodeCfg(t, tree.Nodes["n_assert_token_valid"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status_code", "op": "eq", "expected": float64(200)},
		},
	})
	nodeCfg(t, tree.Nodes["n_assert_token_invalid"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status_code", "op": "eq", "expected": float64(401)},
		},
	})
	nodeCfg(t, tree.Nodes["n_assert_register"], map[string]any{
		"assertions": []any{
			map[string]any{"field": "status_code", "op": "eq", "expected": float64(200)},
		},
	})

	// Inputs for api nodes (mirroring the real flow)
	tree.Nodes["n_login"].Inputs = map[string]flow.IOKey{
		"username": {Type: "string", Source: "$cache.username"},
		"password": {Type: "string", Source: "$cache.password"},
	}
	tree.Nodes["n_token_valid"].Inputs = map[string]flow.IOKey{
		"auth": {Type: "string", Source: "$cache.token"},
	}
	tree.Nodes["n_logout"].Inputs = map[string]flow.IOKey{
		"auth": {Type: "string", Source: "$cache.token"},
	}
	tree.Nodes["n_token_invalid"].Inputs = map[string]flow.IOKey{
		"auth": {Type: "string", Source: "$cache.token"},
	}
	tree.Nodes["n_register"].Inputs = map[string]flow.IOKey{
		"username": {Type: "string", Source: "$cache.username"},
		"password": {Type: "string", Source: "$cache.password"},
	}

	// Unit configs for api nodes
	nodeCfg(t, tree.Nodes["n_login"], map[string]any{
		"unit_id": float64(1),
		"unit": map[string]any{
			"method":   "POST",
			"path":     "/auth/login",
			"tag":      "认证",
			"name":     "用户登录",
			"params":   "[]",
			"security": "null",
		},
	})
	nodeCfg(t, tree.Nodes["n_token_valid"], map[string]any{
		"unit_id": float64(40),
		"unit": map[string]any{
			"method":   "GET",
			"path":     "/providers",
			"tag":      "LLM Provider",
			"name":     "列出 LLM Provider",
			"params":   "[]",
			"security": "[{\"BearerAuth\":[]}]",
		},
	})
	nodeCfg(t, tree.Nodes["n_logout"], map[string]any{
		"unit_id": float64(7),
		"unit": map[string]any{
			"method":   "POST",
			"path":     "/auth/logout",
			"tag":      "认证",
			"name":     "退出登录",
			"params":   "[]",
			"security": "[{\"BearerAuth\":[]}]",
		},
	})
	nodeCfg(t, tree.Nodes["n_token_invalid"], map[string]any{
		"unit_id": float64(40),
		"unit": map[string]any{
			"method":   "GET",
			"path":     "/providers",
			"tag":      "LLM Provider",
			"name":     "列出 LLM Provider",
			"params":   "[]",
			"security": "[{\"BearerAuth\":[]}]",
		},
	})
	nodeCfg(t, tree.Nodes["n_register"], map[string]any{
		"unit_id": float64(19),
		"unit": map[string]any{
			"method":   "POST",
			"path":     "/auth/register",
			"tag":      "认证",
			"name":     "注册账号",
			"params":   "[]",
			"security": "null",
		},
	})

	// Track execution order via a counter assigned at execute-time.
	var seq int
	var order []string
	record := func(id string) {
		seq++
		order = append(order, id)
	}

	// Mock API: login succeeds, token_valid succeeds, logout succeeds,
	// token_invalid returns 401 (asserted OK), register returns 400 (assert fails→catch).
	providerCallCount := 0
	callAPI := func(call APICall) (*APIResponse, error) {
		path := call.URL
		switch {
		case containsStr(path, "/auth/login"):
			record("n_login")
			return &APIResponse{StatusCode: 200, Body: map[string]any{
				"id":       float64(14),
				"token":    "tok-deadbeef",
				"username": "testuser2",
			}}, nil
		case containsStr(path, "/providers"):
			providerCallCount++
			if providerCallCount == 1 {
				record("n_token_valid")
				return &APIResponse{StatusCode: 200, Body: map[string]any{"providers": []any{}}}, nil
			}
			record("n_token_invalid")
			return &APIResponse{StatusCode: 401, Body: "未登录或会话已失效"}, nil
		case containsStr(path, "/auth/logout"):
			record("n_logout")
			return &APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
		case containsStr(path, "/auth/register"):
			record("n_register")
			return &APIResponse{StatusCode: 400, Body: "用户名已存在"}, nil
		default:
			return nil, fmt.Errorf("unexpected API call: %s", path)
		}
	}

	res, err := Run(context.Background(), tree, Options{CallAPI: callAPI})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Collect execution order from results.
	type entry struct {
		id     string
		status Status
		sa     string
		err    string
	}
	var entries []entry
	for id, r := range res.Results {
		sa := "(zero)"
		if !r.StartedAt.IsZero() {
			sa = r.StartedAt.Format("15:04:05.000000")
		}
		errStr := ""
		if r.Error != "" {
			errStr = "  ERR=" + trunc(r.Error, 40)
		}
		entries = append(entries, entry{id, r.Status, sa, errStr})
	}

	// Sort by StartedAt (zero values go last).
	sort.Slice(entries, func(i, j int) bool {
		ri, rj := res.Results[entries[i].id], res.Results[entries[j].id]
		zi, zj := ri.StartedAt.IsZero(), rj.StartedAt.IsZero()
		if zi && zj {
			return entries[i].id < entries[j].id
		}
		if zi {
			return false
		}
		if zj {
			return true
		}
		return ri.StartedAt.Before(rj.StartedAt)
	})

	t.Log("=== Flow 13 树结构 ===")
	t.Log("n1 (start)")
	t.Log(" └── n_seed (cache-set)")
	t.Log("      ├── n_login (api)")
	t.Log("      │    ├── n_cache_token (cache-set)")
	t.Log("      │    │    └── n_token_valid (api)")
	t.Log("      │    │         ├── n_assert_token_valid (assert)")
	t.Log("      │    │         └── n_logout (api)")
	t.Log("      │    │              ├── n_token_invalid (api)")
	t.Log("      │    │              │    └── n_assert_token_invalid (assert)")
	t.Log("      │    │              └── n_assert_logout (assert)")
	t.Log("      │    └── n_assert_login (assert)")
	t.Log("      └── n_try_register (try)")
	t.Log("           └── n_register (api)")
	t.Log("                ├── n_assert_register (assert)")
	t.Log("                └── n_catch_register (catch)")
	t.Log("")
	t.Log("=== 实际执行顺序 (按 StartedAt 排序) ===")
	for i, e := range entries {
		t.Logf("%2d. [%-6s] %-28s started=%s%s", i+1, e.status, e.id, e.sa, e.err)
	}

	t.Log("")
	t.Log("=== API 调用顺序 (mock 记录) ===")
	for i, id := range order {
		t.Logf("%2d. %s", i+1, id)
	}

	// Assert: the statuses should match expectations
	// n_seed, n_login, n_cache_token should be "ok" (own execution succeeded)
	// n_token_valid should be "ok" (own execution succeeded)
	// n_logout should be "ok" (own execution succeeded)
	// n_token_invalid should be "ok" (API 返回 401,但不视为失败)
	// n_assert_token_valid should be "ok" (断言 status_code==200 通过)
	// n_assert_logout should be "ok" (断言 status_code==200 通过)
	// n_assert_login should be "ok" (断言 status_code==200 通过)
	// n_assert_token_invalid should be "ok" (断言 status_code==401 通过)
	// n_try_register should NOT be "failed" (containment)
	// n_register should be "ok" (API 返回 400,但不视为失败)
	// n_assert_register should be "failed" (断言 status_code==200 失败)
	// n_catch_register should be "ok" (consumed assertion failure)

	okNodes := []string{"n_seed", "n_login", "n_cache_token", "n_token_valid",
		"n_logout", "n_assert_token_valid", "n_assert_logout", "n_assert_login",
		"n_assert_token_invalid", "n_token_invalid", "n_register", "n_catch_register"}
	for _, id := range okNodes {
		if r, ok := res.Results[id]; ok && r.Status != StatusOK {
			t.Errorf("%s status = %s, want ok", id, r.Status)
		}
	}
	if res.Results["n_try_register"].Status == StatusFailed {
		t.Error("n_try_register should not be failed (try contains failure)")
	}
	if res.Results["n_assert_register"].Status != StatusFailed {
		t.Errorf("n_assert_register status = %s, want failed (断言 status_code==200 对 400 响应失败)", res.Results["n_assert_register"].Status)
	}

	_ = order
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// 断言期望值类型化解析 + 类型感知比较。
func TestAssertionTypedCompare(t *testing.T) {
	cases := []struct {
		name     string
		actual   any
		expected any
		op       string
		want     bool
	}{
		{"数字字面量 vs 数字", 200, "200", "eq", true},
		{"数字 vs 数字字符串", "200", "200", "eq", true},
		{"数字与字符串精确不等(带引号)", 200, `"200"`, "eq", false},
		{"数字不等于", 200, "201", "ne", true},
		{"布尔字面量", true, "true", "eq", true},
		{"布尔 false", false, "false", "eq", true},
		{"null 严格判空", nil, "null", "eq", true},
		{"null 不匹配非空", "x", "null", "eq", false},
		{"字符串裸 token", "abc", "abc", "eq", true},
		{"数组深度相等", []any{1.0, 2.0}, "[1,2]", "eq", true},
		{"数组不等", []any{1.0, 2.0}, "[1,3]", "eq", false},
		{"对象深度相等", map[string]any{"a": float64(1)}, `{"a":1}`, "eq", true},
		{"gt 数字字符串归一", 10, "2", "gt", true},
		{"gt 字符串词典(非数字)", "b", "a", "gt", true},
		{"contains 子串", "hello world", "world", "contains", true},
		{"contains 数组元素", []any{"a", float64(200)}, "200", "contains", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := &engine{}
			got, err := e.evalAssertion(Assertion{Field: "f", Operator: c.op, Expected: c.expected}, map[string]any{"f": c.actual})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("want %v, got %v", c.want, got)
			}
		})
	}
}

// normalizeExpected 解析行为单元测试。
func TestNormalizeExpected(t *testing.T) {
	n := func(v any) any { return normalizeExpected(v) }
	if got := n("200"); got != float64(200) {
		t.Fatalf("数字解析失败: %#v", got)
	}
	if got := n("true"); got != true {
		t.Fatalf("布尔解析失败: %#v", got)
	}
	if got := n("null"); got != nil {
		t.Fatalf("null 解析失败: %#v", got)
	}
	if got := n("abc"); got != "abc" {
		t.Fatalf("裸字符串应原样保留: %#v", got)
	}
	if got := n(`"abc"`); got != "abc" {
		t.Fatalf("带引号字符串解析失败: %#v", got)
	}
	if got := n("2500"); got != float64(2500) {
		t.Fatalf("空串应保留: %#v", got)
	}
}

// TestAPICustomHeaders 验证 api 节点的 config.headers:字面量直接写入,
// '=' 前缀按 JSONata 对当前输入求值,且自定义头覆盖同名自动认证头。
func TestAPICustomHeaders(t *testing.T) {
	var got APICall
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
	})
	nodeCfg(t, tree.Nodes["n2"], map[string]any{
		"unit": map[string]any{"method": "POST", "path": "/llm"},
		"headers": map[string]any{
			"Content-Type":  "application/json",
			"OpenAI-Org":    "org-123",
			"X-Token-Ref":   "=rt",
			"X-Count":       float64(3),
			"Authorization": "Bearer override",
		},
	})
	// 认证输入 auth 会被自动转成 Authorization,自定义头应覆盖它。
	tree.Nodes["n2"].Inputs = map[string]flow.IOKey{
		"auth": {Type: flow.IOTypePrimitive},
		"rt":   {Type: flow.IOTypePrimitive},
	}
	tree.AddChild("n1", "n2")
	res, err := Run(context.Background(), tree, Options{
		Host:        "http://x",
		StartParams: map[string]any{"auth": "abc123", "rt": "resolved-token"},
		CallAPI: func(call APICall) (*APIResponse, error) {
			got = call
			return &APIResponse{StatusCode: 200, Body: map[string]any{}}, nil
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusOK {
		for id, r := range res.Results {
			t.Logf("node %s: status=%s err=%v", id, r.Status, r.Error)
		}
		t.Fatalf("status = %s", res.Status)
	}
	want := map[string]string{
		"Content-Type":  "application/json",
		"OpenAI-Org":    "org-123",
		"X-Token-Ref":   "resolved-token",
		"X-Count":       "3",
		"Authorization": "Bearer override",
	}
	for k, v := range want {
		if got.Headers[k] != v {
			t.Fatalf("header %s = %q, want %q (all: %v)", k, got.Headers[k], v, got.Headers)
		}
	}
}

// callAPINode runs a single api node and returns the captured request.
// inputs lists the node's input keys; start provides runtime values for them.
func callAPINode(t *testing.T, opt struct {
	method     string
	path       string
	security   string
	paramsJSON string
	params     map[string]any // node config.params (explicit params)
	headers    map[string]any // node config.headers (custom headers)
	inputs     []string
	start      map[string]any
}) APICall {
	t.Helper()
	var got APICall
	tree := treeOf(map[string]*flow.Node{
		"n1": flow.NewNode("n1", flow.NodeStart),
		"n2": flow.NewNode("n2", flow.NodeAPI),
	})
	unit := map[string]any{"method": opt.method, "path": opt.path}
	if opt.security != "" {
		unit["security"] = opt.security
	}
	if opt.paramsJSON != "" {
		unit["params"] = opt.paramsJSON
	}
	nodeCfg(t, tree.Nodes["n2"], map[string]any{"unit": unit, "params": opt.params, "headers": opt.headers})
	if len(opt.inputs) > 0 {
		io := map[string]flow.IOKey{}
		for _, k := range opt.inputs {
			io[k] = flow.IOKey{Type: flow.IOTypePrimitive}
		}
		tree.Nodes["n2"].Inputs = io
	}
	tree.AddChild("n1", "n2")
	res, err := Run(context.Background(), tree, Options{
		Host:        "http://x",
		StartParams: opt.start,
		CallAPI: func(c APICall) (*APIResponse, error) {
			got = c
			return &APIResponse{StatusCode: 200, Body: map[string]any{"ok": true}}, nil
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %s", res.Status)
	}
	return got
}

func TestAPIParamInHeaderRouting(t *testing.T) {
	const signParams = `[{"name":"X-Timestamp","in":"header"},{"name":"X-Nonce","in":"header"},{"name":"X-Signature","in":"header"}]`
	got := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "POST", path: "/utils/sign", paramsJSON: signParams,
		inputs: []string{"X-Timestamp", "X-Nonce", "X-Signature"},
		start:  map[string]any{"X-Timestamp": float64(1700000000), "X-Nonce": "abc", "X-Signature": "deadbeef"},
	})
	// 三个 in:header 参数必须作为请求头送达,不得塞进 body。
	if got.Headers["X-Timestamp"] != "1700000000" {
		t.Fatalf("X-Timestamp header = %q, want numeric string (all: %v)", got.Headers["X-Timestamp"], got.Headers)
	}
	if got.Headers["X-Nonce"] != "abc" || got.Headers["X-Signature"] != "deadbeef" {
		t.Fatalf("sign headers = %v", got.Headers)
	}
	if b, ok := got.Body.(map[string]any); ok {
		if _, has := b["X-Timestamp"]; has {
			t.Fatalf("in:header param must not be routed into body: %v", b)
		}
	}
}

func TestAPIParamInQueryAndBody(t *testing.T) {
	const paramsJSON = `[{"name":"q","in":"query"},{"name":"payload","in":"body"}]`
	got := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "POST", path: "/search", paramsJSON: paramsJSON,
		inputs: []string{"q", "payload"},
		start:  map[string]any{"q": "golang", "payload": map[string]any{"a": 1}},
	})
	if got.Query["q"] != "golang" {
		t.Fatalf("in:query param = %v, want query", got.Query)
	}
	if b, ok := got.Body.(map[string]any); ok {
		if b["payload"] == nil {
			t.Fatalf("in:body param missing: %v", got.Body)
		}
	} else {
		t.Fatalf("body = %v", got.Body)
	}
}

func TestAPIParamInCoexistWithAuthAndSignatureOnly(t *testing.T) {
	const signParams = `[{"name":"X-Timestamp","in":"header"},{"name":"X-Nonce","in":"header"},{"name":"X-Signature","in":"header"}]`
	// 并存:auth 输入派生 Authorization,同时 X-* 走 in:header,互不覆盖。
	got := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "POST", path: "/utils/sign", security: `[{"BearerAuth":[]}]`, paramsJSON: signParams,
		inputs: []string{"auth", "X-Timestamp", "X-Nonce", "X-Signature"},
		start:  map[string]any{"auth": "tok", "X-Timestamp": float64(1), "X-Nonce": "a", "X-Signature": "b"},
	})
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("auth header = %q, want Bearer tok (all: %v)", got.Headers["Authorization"], got.Headers)
	}
	for _, h := range []string{"X-Timestamp", "X-Nonce", "X-Signature"} {
		if got.Headers[h] == "" {
			t.Fatalf("coexist header %s missing: %v", h, got.Headers)
		}
	}
	// 仅签名:无认证输入,只有 in:header,不得产生任何认证头。
	got2 := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "POST", path: "/utils/sign", paramsJSON: signParams,
		inputs: []string{"X-Timestamp", "X-Nonce", "X-Signature"},
		start:  map[string]any{"X-Timestamp": float64(1), "X-Nonce": "a", "X-Signature": "b"},
	})
	if _, has := got2.Headers["Authorization"]; has {
		t.Fatalf("signature-only must not send auth header: %v", got2.Headers)
	}
	if got2.Headers["X-Signature"] != "b" {
		t.Fatalf("signature-only headers = %v", got2.Headers)
	}
}

func TestAPIParamInSameKeyConflict(t *testing.T) {
	// 某键既是 in:header 参数又匹配认证键名(Authorization):以声明位置为准,
	// 按原值作为请求头,不重复走 Bearer 前缀;config.headers 最终覆盖。
	got := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "GET", path: "/x", paramsJSON: `[{"name":"Authorization","in":"header"}]`,
		inputs: []string{"Authorization"},
		start:  map[string]any{"Authorization": "raw-token"},
	})
	if got.Headers["Authorization"] != "raw-token" {
		t.Fatalf("same-key Authorization should route as-is, got %q (all: %v)", got.Headers["Authorization"], got.Headers)
	}
	// config.headers 最终覆盖同键。
	got2 := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "GET", path: "/x", paramsJSON: `[{"name":"Authorization","in":"header"}]`,
		inputs:  []string{"Authorization"},
		start:   map[string]any{"Authorization": "raw"},
		headers: map[string]any{"Authorization": "override"},
	})
	if got2.Headers["Authorization"] != "override" {
		t.Fatalf("config.headers should override same key: %v", got2.Headers)
	}
}

func TestAPIParamInUndeclaredFallsBackToHeuristic(t *testing.T) {
	// 未在 params 声明的 isAuthKey 键(auth)回退启发式 → Bearer 认证头。
	got := callAPINode(t, struct {
		method     string
		path       string
		security   string
		paramsJSON string
		params     map[string]any
		headers    map[string]any
		inputs     []string
		start      map[string]any
	}{
		method: "POST", path: "/utils/sign", security: `[{"BearerAuth":[]}]`,
		inputs: []string{"auth"},
		start:  map[string]any{"auth": "tok"},
	})
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("undeclared auth key should fall back to Bearer: %v", got.Headers)
	}
}
