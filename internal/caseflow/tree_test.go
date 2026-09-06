package caseflow

import "testing"

func TestValidateCaseTree(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tree    Tree
		wantErr bool
	}{
		{"valid", Tree{Root: &Node{ID: "r", Children: []*Node{{ID: "a"}, {ID: "b"}}}}, false},
		{"missing root", Tree{}, true},
		{"duplicate", Tree{Root: &Node{ID: "r", Children: []*Node{{ID: "a"}, {ID: "a"}}}}, true},
		{"invalid status", Tree{Root: &Node{ID: "r", Status: "pending"}}, true},
		{"nil child", Tree{Root: &Node{ID: "r", Children: []*Node{nil}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (Validate(tc.tree) != nil) != tc.wantErr {
				t.Fatalf("Validate(%s) mismatch", tc.name)
			}
		})
	}
}

func TestDescendantsIncludesRootAndAllChildren(t *testing.T) {
	root := &Node{ID: "r", Children: []*Node{{ID: "a", Children: []*Node{{ID: "x"}}}}}
	got := Descendants(root)
	if len(got) != 3 || got[0].ID != "r" || got[2].ID != "x" {
		t.Fatalf("unexpected descendants: %#v", got)
	}
}
