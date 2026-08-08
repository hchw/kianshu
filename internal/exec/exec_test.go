package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
		"n3": flow.NewNode("n3", flow.NodeAdapter),  // branch root
		"n4": flow.NewNode("n4", flow.NodeAPI),      // protected, fails
		"n5": flow.NewNode("n5", flow.NodeCatch),    // branch-local catch
		"n6": flow.NewNode("n6", flow.NodeAPI),      // catch child (side effect)
		"n7": flow.NewNode("n7", flow.NodeAdapter),  // sibling of try
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
		CallAPI: func(c APICall) (any, error) {
			apiCalls++
			if strings.HasSuffix(c.URL, "/ok") {
				return map[string]any{"ok": true}, nil
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
	nodeCfg(t, tree.Nodes["n3"], map[string]any{"writes": map[string]any{"token": "$.resp.token"}})
	nodeCfg(t, tree.Nodes["n4"], map[string]any{"expr": `$.token`})
	tree.Nodes["n4"].Inputs = map[string]flow.IOKey{"token": {Type: flow.IOTypePrimitive, Source: "$cache.token"}}
	tree.AddChild("n1", "n2")
	tree.AddChild("n2", "n3")
	tree.AddChild("n2", "n4") // reader 是兄弟,非子节点,先写后读

	res, _ := Run(context.Background(), tree, Options{
		Host: "https://api.example.com",
		CallAPI: func(c APICall) (any, error) {
			return map[string]any{"resp": map[string]any{"token": "abc123"}}, nil
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
		"account":     {Type: flow.IOTypePrimitive},
		"authorization": {Type: flow.IOTypePrimitive},
	}
	tree.AddChild("n1", "n2")

	res, _ := Run(context.Background(), tree, Options{
		Host: "https://api.example.com/",
		StartParams: map[string]any{
			"account":       "alice",
			"authorization": "Bearer t",
		},
		CallAPI: func(c APICall) (any, error) {
			got = c
			return map[string]any{"ok": true}, nil
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
		CallAPI: func(c APICall) (any, error) {
			got = c
			return map[string]any{"ok": true}, nil
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
