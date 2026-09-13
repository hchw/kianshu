package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github/hchw/kianshu/internal/flow"

	"gorm.io/gorm"
)

// CaseUnitAnchor links a selected case leaf to the pre-built case-unit node
// that anchors its execution branch.
type CaseUnitAnchor struct {
	Leaf     CaseBindingLeaf
	AnchorID string
}

func genCaseUnitID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "cu" + hex.EncodeToString(b)
}

// BuildCaseUnitSkeleton pre-creates one case-unit anchor per case leaf under
// the start node, in case-tree traversal order (B1: decomposition nodes do not
// become nodes; leaves are the cases). It mutates the tree in memory and does
// not persist it.
func BuildCaseUnitSkeleton(tree *flow.Tree, binding *CaseBinding) ([]CaseUnitAnchor, error) {
	if tree == nil || tree.Nodes == nil {
		return nil, errors.New("执行流草稿为空")
	}
	if tree.Nodes[tree.Start] == nil {
		return nil, errors.New("缺少 start 节点")
	}
	anchors := make([]CaseUnitAnchor, 0, len(binding.Leaves))
	for _, leaf := range binding.Leaves {
		id := genCaseUnitID()
		cfg, err := flow.MarshalConfig(flow.CaseUnitConfig{
			CaseFlowID:    leaf.CaseFlowID,
			CaseVersionNo: leaf.CaseVersionNo,
			CaseNodeID:    leaf.CaseNodeID,
			Title:         leaf.Title,
		})
		if err != nil {
			return nil, err
		}
		n := flow.NewNode(id, flow.NodeCaseUnit)
		n.Config = cfg
		tree.Nodes[id] = n
		tree.AddChild(tree.Start, id)
		anchors = append(anchors, CaseUnitAnchor{Leaf: leaf, AnchorID: id})
	}
	return anchors, nil
}

// flowDescendants returns every descendant id of the node, depth-first.
func flowDescendants(tree *flow.Tree, id string) []string {
	out := []string{}
	var walk func(string)
	walk = func(x string) {
		n := tree.Nodes[x]
		if n == nil {
			return
		}
		for _, c := range n.Children {
			out = append(out, c)
			walk(c)
		}
	}
	walk(id)
	return out
}

// removeCaseUnits drops every existing case-unit anchor and its whole subtree,
// so a re-generation starts from a clean skeleton without duplicating anchors.
func removeCaseUnits(tree *flow.Tree) {
	for _, id := range flow.Keys(tree.Nodes) {
		n := tree.Nodes[id]
		if n == nil || n.Type != flow.NodeCaseUnit {
			continue
		}
		parent := tree.Nodes[n.Parent]
		for _, d := range append([]string{id}, flowDescendants(tree, id)...) {
			delete(tree.Nodes, d)
		}
		if parent != nil {
			kept := parent.Children[:0]
			for _, c := range parent.Children {
				if c != id {
					kept = append(kept, c)
				}
			}
			parent.Children = kept
		}
	}
}

// PrebuildCaseUnits resets any previous skeleton and pre-creates case-unit
// anchors for the flow's bound case leaves, persisting the result to the draft.
func PrebuildCaseUnits(db *gorm.DB, flowID uint, binding *CaseBinding) ([]CaseUnitAnchor, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	removeCaseUnits(tree)
	anchors, err := BuildCaseUnitSkeleton(tree, binding)
	if err != nil {
		return nil, err
	}
	if _, err := UpdateDraft(db, flowID, d.Name, tree.String(), nil, ""); err != nil {
		return nil, err
	}
	return anchors, nil
}

// MissingCaseUnits reports bound case leaves that have no case-unit anchor
// with at least one child in the given tree — i.e. cases left unimplemented.
func MissingCaseUnits(tree *flow.Tree, binding *CaseBinding) []CaseBindingLeaf {
	impl := map[string]bool{}
	for _, n := range tree.CaseUnitNodes() {
		cfg, ok := flow.CaseUnitBinding(n)
		if !ok {
			continue
		}
		if len(n.Children) == 0 {
			continue
		}
		impl[fmt.Sprintf("%d:%s", cfg.CaseFlowID, cfg.CaseNodeID)] = true
	}
	var missing []CaseBindingLeaf
	for _, leaf := range binding.Leaves {
		if !impl[fmt.Sprintf("%d:%s", leaf.CaseFlowID, leaf.CaseNodeID)] {
			missing = append(missing, leaf)
		}
	}
	return missing
}
