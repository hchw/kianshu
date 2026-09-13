package exec

import (
	"context"
	"testing"

	"github/hchw/kianshu/internal/flow"
)

// TestCaseUnitAggregatesAndIsolatesBranches verifies the case-unit node is a
// pure passthrough grouping: its status aggregates its children, and a failing
// branch does not stop the other branches.
func TestCaseUnitAggregatesAndIsolatesBranches(t *testing.T) {
	tree := treeOf(map[string]*flow.Node{
		"n1":  flow.NewNode("n1", flow.NodeStart),
		"cu1": flow.NewNode("cu1", flow.NodeCaseUnit),
		"cu2": flow.NewNode("cu2", flow.NodeCaseUnit),
		"cu3": flow.NewNode("cu3", flow.NodeCaseUnit),
		"a1":  flow.NewNode("a1", flow.NodeAdapter),
		"a2":  flow.NewNode("a2", flow.NodeAdapter),
		"a3":  flow.NewNode("a3", flow.NodeAdapter),
	})
	nodeCfg(t, tree.Nodes["a1"], map[string]any{"expr": `$.n * 2`})
	nodeCfg(t, tree.Nodes["a2"], map[string]any{"expr": `(`}) // invalid JSONata → fails
	nodeCfg(t, tree.Nodes["a3"], map[string]any{"expr": `$.n * 3`})
	tree.AddChild("n1", "cu1")
	tree.AddChild("cu1", "a1")
	tree.AddChild("n1", "cu2")
	tree.AddChild("cu2", "a2")
	tree.AddChild("n1", "cu3")
	tree.AddChild("cu3", "a3")

	res, err := Run(context.Background(), tree, Options{StartParams: map[string]any{"n": float64(5)}})
	if err != nil {
		t.Fatal(err)
	}

	if res.Results["a1"].Status != StatusOK {
		t.Fatalf("a1 = %s, want ok", res.Results["a1"].Status)
	}
	if res.Results["cu1"].Status != StatusOK {
		t.Fatalf("cu1 (all children ok) = %s, want ok", res.Results["cu1"].Status)
	}
	if res.Results["a2"].Status != StatusFailed {
		t.Fatalf("a2 = %s, want failed", res.Results["a2"].Status)
	}
	if res.Results["cu2"].Status != StatusFailed {
		t.Fatalf("cu2 (child failed) = %s, want failed", res.Results["cu2"].Status)
	}
	// Isolation: the failure inside cu2 must not stop cu3.
	if res.Results["a3"].Status != StatusOK || res.Results["cu3"].Status != StatusOK {
		t.Fatalf("cu3/a3 should still run, got cu3=%s a3=%s", res.Results["cu3"].Status, res.Results["a3"].Status)
	}
	if res.Status != StatusFailed {
		t.Fatalf("run status = %s, want failed", res.Status)
	}
}
