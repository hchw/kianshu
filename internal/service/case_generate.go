package service

import (
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/flow"

	"gorm.io/gorm"
)

// BuildCaseGenerationContext assembles the structured prompt for case-driven
// generation: the case hierarchy with all fields, the bound sources, the
// available test units and the pre-built case-unit anchors the LLM must fill.
func BuildCaseGenerationContext(db *gorm.DB, flowID uint, binding *CaseBinding, anchors []CaseUnitAnchor) string {
	var b strings.Builder
	b.WriteString("从用例流生成执行流。系统已按用例树预建分支锚点（case-unit），")
	b.WriteString("你只能在每个锚点内部创建执行节点，不得新建、删除或移动顶层分支锚点。\n\n")

	b.WriteString("## 用例结构（层级 + 全部字段）\n")
	anchorOf := map[string]string{}
	for _, a := range anchors {
		anchorOf[fmt.Sprintf("%d:%s", a.Leaf.CaseFlowID, a.Leaf.CaseNodeID)] = a.AnchorID
	}
	for _, leaf := range binding.Leaves {
		anchor := anchorOf[fmt.Sprintf("%d:%s", leaf.CaseFlowID, leaf.CaseNodeID)]
		path := leaf.Path
		if path == "" {
			path = leaf.Title
		}
		fmt.Fprintf(&b, "- %s  [锚点 %s]\n", path, anchor)
		if leaf.Description != "" {
			fmt.Fprintf(&b, "    描述：%s\n", leaf.Description)
		}
		if leaf.Precondition != "" {
			fmt.Fprintf(&b, "    前置条件：%s\n", leaf.Precondition)
		}
		if leaf.Input != "" {
			fmt.Fprintf(&b, "    输入：%s\n", leaf.Input)
		}
		if leaf.Expected != "" {
			fmt.Fprintf(&b, "    预期结果：%s\n", leaf.Expected)
		}
	}

	b.WriteString("\n## 绑定来源\n")
	for _, src := range binding.Sources {
		fmt.Fprintf(&b, "### 用例流「%s」版本 v%d\n", src.Name, src.VersionNo)
		for _, rs := range binding.Resolved[src.CaseFlowID] {
			switch rs.Kind {
			case "document":
				if rs.Document != nil {
					fmt.Fprintf(&b, "#### 背景文档：%s\n%s\n", rs.Document.Name, rs.Document.Content)
				} else {
					b.WriteString("#### 背景文档：不可用（已被删除）\n")
				}
			case "all", "tag":
				fmt.Fprintf(&b, "#### 接口范围 %s：匹配 %d 个测试单元\n", rs.Kind, len(rs.UnitIDs))
			}
		}
	}

	tsID := flowTestSetID(db, flowID)
	if units, err := listUnitsQuery(db, tsID, "", ""); err == nil {
		b.WriteString("\n## 可用测试单元（unit_id=ID）\n")
		b.WriteString(formatUnitBriefs(units))
	}

	b.WriteString("\n## 要求\n")
	b.WriteString("1. 每个锚点内部创建一条自包含的执行分支：认证、取 token、写缓存、断言都在该分支内。\n")
	b.WriteString("2. 共享的静态初值（账号、密码、base 参数）写入 start 的 config.params。\n")
	b.WriteString("3. 不得依赖其他锚点内部写入的缓存；跨分支引用缓存会被校验拒绝。\n")
	b.WriteString("4. 每个锚点至少创建一个执行节点，否则该用例视为未实现。\n")
	b.WriteString("5. 保持锚点与用例的对应关系，不要新建顶层分支。\n")
	return b.String()
}

// verifyCaseCoverage loads the current draft and returns the bound cases that
// have no case-unit branch with at least one execution node.
func verifyCaseCoverage(db *gorm.DB, flowID uint, binding *CaseBinding) []CaseBindingLeaf {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return binding.Leaves
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return binding.Leaves
	}
	return MissingCaseUnits(tree, binding)
}

// FlowCaseSourcesView is the case-source view of an execution flow.
type FlowCaseSourcesView struct {
	Bound   bool         `json:"bound"`
	Binding *CaseBinding `json:"binding,omitempty"`
}

// FlowCaseSources returns the case binding of a flow (empty when the flow was
// created blank).
func FlowCaseSources(db *gorm.DB, flowID uint) (*FlowCaseSourcesView, error) {
	b, ok, err := GetFlowCaseBinding(db, flowID)
	if err != nil {
		return nil, err
	}
	return &FlowCaseSourcesView{Bound: ok, Binding: b}, nil
}
