// Package flow implements the execution tree model of a test flow: node
// types, I/O contracts, JSONata adapters, whole-tree validation, and the
// draft/version lifecycle helpers built on the tree JSON representation.
package flow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NodeType enumerates the supported execution node kinds.
type NodeType string

const (
	NodeStart    NodeType = "start"
	NodeAPI      NodeType = "api"
	NodeAssert   NodeType = "assert"
	NodeLoop     NodeType = "loop"
	NodeTry      NodeType = "try"
	NodeCatch    NodeType = "catch"
	NodeCacheSet NodeType = "cache-set"
	NodeAdapter  NodeType = "adapter"
)

// IOType enumerates the typed-key value kinds in a node's I/O contract.
type IOType string

const (
	IOTypePrimitive IOType = "primitive"
	IOTypeObject    IOType = "object"
	IOTypeArray     IOType = "array"
)

// IOKey describes one input or output key with its type and optional source.
type IOKey struct {
	Type   IOType `json:"type"`
	Source string `json:"source,omitempty"` // e.g. "$cache.key" or an ancestor output key
}

// Node is one vertex of the execution tree.
type Node struct {
	ID       string           `json:"id"`
	Type     NodeType         `json:"type"`
	Parent   string           `json:"parent,omitempty"`
	Children []string         `json:"children,omitempty"`
	Inputs   map[string]IOKey `json:"inputs,omitempty"`
	Outputs  map[string]IOKey `json:"outputs,omitempty"`
	Config   json.RawMessage  `json:"config,omitempty"`
	// X/Y are optional canvas coordinates (pointer so nil = not placed by the
	// user; the editor auto-lays out unplaced nodes). Pure presentation data:
	// execution ignores them.
	X *float64 `json:"x,omitempty"`
	Y *float64 `json:"y,omitempty"`
}

// Tree is the whole-tree JSON representation stored in drafts and versions.
type Tree struct {
	Start     string           `json:"start"`
	Nodes     map[string]*Node `json:"nodes"`
	CacheSets []string         `json:"cacheSets,omitempty"`
}

// Config is the raw JSON config payload of a node. Typed accessors below
// decode it into concrete per-type configs.
type Config struct {
	raw json.RawMessage
}

// UnmarshalConfig decodes a node's config payload into the given struct.
func UnmarshalConfig(n *Node, v any) error {
	if len(n.Config) == 0 {
		return nil
	}
	return json.Unmarshal(n.Config, v)
}

// MarshalConfig encodes a per-type config into a node's config payload.
func MarshalConfig(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ParseTree decodes a tree from its JSON string.
func ParseTree(s string) (*Tree, error) {
	t := &Tree{}
	if err := json.Unmarshal([]byte(s), t); err != nil {
		return nil, fmt.Errorf("解析执行树失败: %w", err)
	}
	return t, nil
}

// String serializes the tree to its canonical JSON string.
func (t *Tree) String() string {
	b, err := json.Marshal(t)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ValidateTreeShape checks the structural constraints that hold for every
// tree: exactly one root (the start node), a single parent per node, no
// cycles, and cache-set nodes kept out of the link structure.
func (t *Tree) ValidateTreeShape() []ValidationError {
	// Reconcile the two link encodings: parent pointers are authoritative and
	// children lists are derived from them, so clients may submit either.
	t.reconcileChildren()

	var errs []ValidationError
	if t.Nodes == nil {
		return append(errs, ValidationError{Code: "tree.empty", Message: "树为空"})
	}
	root, ok := t.Nodes[t.Start]
	if !ok || root == nil {
		return append(errs, ValidationError{Code: "tree.start_missing", Message: "缺少 start 根节点"})
	}

	// Structural sanity for each node, then reachability + cycle + single
	// parent checks via a DFS from the start root following children.
	// cache-set nodes are exempt: they participate without tree links.
	isCacheSet := map[string]bool{}
	for _, cs := range t.CacheSets {
		isCacheSet[cs] = true
	}
	for id, n := range t.Nodes {
		if n == nil {
			errs = append(errs, ValidationError{NodeID: id, Code: "node.nil", Message: "节点为空"})
			continue
		}
		if id == t.Start {
			continue
		}
		if isCacheSet[id] {
			if n.Parent != "" || len(n.Children) > 0 {
				errs = append(errs, ValidationError{NodeID: id, Code: "cache.linked", Message: "cache-set 节点不得参与树连线"})
			}
			continue
		}
		if n.Parent == "" {
			errs = append(errs, ValidationError{NodeID: id, Code: "tree.no_parent", Message: "非 start 节点缺少父节点"})
			continue
		}
		if _, exists := t.Nodes[n.Parent]; !exists {
			errs = append(errs, ValidationError{NodeID: id, Code: "node.parent_missing", Message: "父节点不存在: " + n.Parent})
		}
	}

	// DFS from the root with three-color marking: a back-edge to a grey
	// (in-progress) node is a cycle; any node never reached is disconnected.
	// Parent pointers are single-valued, so two-parent clashes cannot occur.
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	stack = append(stack, t.Start)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		if color[id] == white {
			color[id] = grey
			n := t.Nodes[id]
			if n != nil {
				for _, c := range n.Children {
					if color[c] == grey {
						errs = append(errs, ValidationError{NodeID: c, Code: "tree.cycle", Message: "检测到环路"})
						continue
					}
					if color[c] == white {
						stack = append(stack, c)
					}
				}
			}
		} else {
			color[id] = black
			stack = stack[:len(stack)-1]
		}
	}
	for id := range t.Nodes {
		if _, ok := color[id]; !ok || color[id] != black {
			if isCacheSet[id] {
				continue
			}
			errs = append(errs, ValidationError{NodeID: id, Code: "tree.disconnected", Message: "节点不属于 start 根树"})
		}
	}

	// cache-set nodes must not participate in parent/children links.
	for _, cs := range t.CacheSets {
		n, ok := t.Nodes[cs]
		if !ok || n == nil {
			errs = append(errs, ValidationError{NodeID: cs, Code: "cache.node_missing", Message: "cache-set 节点不存在"})
			continue
		}
		if n.Parent != "" || len(n.Children) > 0 {
			errs = append(errs, ValidationError{NodeID: cs, Code: "cache.linked", Message: "cache-set 节点不得参与树连线"})
		}
	}

	return errs
}

// Ancestors returns the chain of ancestor node IDs from root to the given
// node (excluding the node itself). The result is in root-first order.
func (t *Tree) Ancestors(id string) []string {
	var chain []string
	cur := id
	for {
		n, ok := t.Nodes[cur]
		if !ok || n == nil || n.Parent == "" {
			break
		}
		chain = append([]string{n.Parent}, chain...)
		cur = n.Parent
	}
	return chain
}

// IsDescendant reports whether `maybe` lies under `ancestor` in the tree.
func (t *Tree) IsDescendant(ancestor, maybe string) bool {
	for _, a := range t.Ancestors(maybe) {
		if a == ancestor {
			return true
		}
	}
	return false
}

// NewNode builds a node with the given type and canonical I/O contract.
func NewNode(id string, typ NodeType) *Node {
	return &Node{ID: id, Type: typ, Inputs: map[string]IOKey{}, Outputs: map[string]IOKey{}}
}

// reconcileChildren rebuilds every node's children list from parent pointers,
// making the parent encoding authoritative regardless of which form a client
// submitted.
func (t *Tree) reconcileChildren() {
	for _, n := range t.Nodes {
		if n != nil {
			n.Children = nil
		}
	}
	for id, n := range t.Nodes {
		if n == nil || n.Parent == "" {
			continue
		}
		p, ok := t.Nodes[n.Parent]
		if !ok || p == nil {
			continue
		}
		found := false
		for _, c := range p.Children {
			if c == id {
				found = true
				break
			}
		}
		if !found {
			p.Children = append(p.Children, id)
		}
	}
}

// AddChild links `child` under `parent`, maintaining parent/children edges.
func (t *Tree) AddChild(parent, child string) {
	pn, ok := t.Nodes[parent]
	if !ok {
		return
	}
	cn, ok := t.Nodes[child]
	if !ok {
		return
	}
	cn.Parent = parent
	for _, c := range pn.Children {
		if c == child {
			return
		}
	}
	pn.Children = append(pn.Children, child)
}

// Keys returns the sorted keys of a map.
func Keys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CacheKey strips a $cache. prefix from an input source reference.
func CacheKey(source string) (string, bool) {
	if strings.HasPrefix(source, "$cache.") {
		return strings.TrimPrefix(source, "$cache."), true
	}
	return "", false
}
