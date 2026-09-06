package caseflow

import "fmt"

const (
	StatusCovered   = "covered"
	StatusUncovered = "uncovered"
)

type Node struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Precondition string   `json:"precondition,omitempty"`
	Input        string   `json:"input,omitempty"`
	Expected     string   `json:"expected,omitempty"`
	Status       string   `json:"status"`
	X            *float64 `json:"x,omitempty"`
	Y            *float64 `json:"y,omitempty"`
	Children     []*Node  `json:"children,omitempty"`
}

type Tree struct {
	Root *Node `json:"root"`
}

// Validate enforces one rooted, acyclic tree with unique IDs and consistent parents.
func Validate(t Tree) error {
	if t.Root == nil || t.Root.ID == "" {
		return fmt.Errorf("case tree must have one root")
	}
	seen := map[string]bool{}
	var visit func(*Node) error
	visit = func(n *Node) error {
		if n == nil || n.ID == "" {
			return fmt.Errorf("case node is invalid")
		}
		if seen[n.ID] {
			return fmt.Errorf("case tree contains duplicate or cycle: %s", n.ID)
		}
		seen[n.ID] = true
		if n.Status != "" && n.Status != StatusCovered && n.Status != StatusUncovered {
			return fmt.Errorf("invalid case status: %s", n.Status)
		}
		for _, child := range n.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(t.Root)
}

func Find(root *Node, id string) *Node {
	if root == nil {
		return nil
	}
	if root.ID == id {
		return root
	}
	for _, child := range root.Children {
		if found := Find(child, id); found != nil {
			return found
		}
	}
	return nil
}

func RemoveFrom(root *Node, id string) (*Node, bool) {
	if root == nil {
		return nil, false
	}
	for i, child := range root.Children {
		if child != nil && child.ID == id {
			root.Children = append(root.Children[:i], root.Children[i+1:]...)
			return child, true
		}
		if removed, ok := RemoveFrom(child, id); ok {
			return removed, true
		}
	}
	return nil, false
}

func Descendants(root *Node) []*Node {
	if root == nil {
		return nil
	}
	out := []*Node{root}
	for _, child := range root.Children {
		out = append(out, Descendants(child)...)
	}
	return out
}
