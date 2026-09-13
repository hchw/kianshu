package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	"gorm.io/gorm"
)

// CaseSelection is one subtree-root selection for execution flow generation.
// VersionNo pins the case flow version to bind; 0 means "latest saved version".
type CaseSelection struct {
	CaseFlowID uint     `json:"case_flow_id"`
	VersionNo  int      `json:"version_no,omitempty"`
	RootIDs    []string `json:"root_ids"`
}

// CaseBindingSource records one bound case flow version.
type CaseBindingSource struct {
	CaseFlowID uint   `json:"case_flow_id"`
	Name       string `json:"name"`
	VersionNo  int    `json:"version_no"`
}

// CaseBindingLeaf is one selected case leaf expanded from the subtree roots.
type CaseBindingLeaf struct {
	CaseFlowID    uint   `json:"case_flow_id"`
	CaseVersionNo int    `json:"case_version_no"`
	CaseNodeID    string `json:"case_node_id"`
	Title         string `json:"title"`
	Path          string `json:"path,omitempty"`
	Description   string `json:"description,omitempty"`
	Precondition  string `json:"precondition,omitempty"`
	Input         string `json:"input,omitempty"`
	Expected      string `json:"expected,omitempty"`
}

// CaseBinding is the persisted generation-source binding of a case-derived
// execution flow draft: the bound case flow versions, the selected subtree
// roots, the expanded case leaves and the resolved source snapshots.
type CaseBinding struct {
	Sources  []CaseBindingSource       `json:"sources"`
	Roots    []CaseSelection           `json:"roots"`
	Leaves   []CaseBindingLeaf         `json:"leaves"`
	Resolved map[uint][]ResolvedSource `json:"resolved,omitempty"`
}

// resolveCaseVersion resolves the version to bind: an explicit version when
// given, otherwise the latest saved version. A case flow without any saved
// version is rejected — a mutable draft must never be a generation source.
func resolveCaseVersion(db *gorm.DB, caseFlowID uint, versionNo int) (*model.CaseFlowVersion, error) {
	var v model.CaseFlowVersion
	q := db.Where("case_flow_id = ?", caseFlowID)
	if versionNo > 0 {
		q = q.Where("version_no = ?", versionNo)
	}
	if err := q.Order("version_no desc").First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 用例流 %d", ErrCaseFlowVersionRequired, caseFlowID)
		}
		return nil, err
	}
	return &v, nil
}

// casePath returns the "A / B / C" title path from root to the node.
func casePath(root *caseflow.Node, id string) string {
	var walk func(n *caseflow.Node, trail []string) string
	walk = func(n *caseflow.Node, trail []string) string {
		if n == nil {
			return ""
		}
		next := append(append([]string{}, trail...), n.Title)
		if n.ID == id {
			return strings.Join(next, " / ")
		}
		for _, c := range n.Children {
			if p := walk(c, next); p != "" {
				return p
			}
		}
		return ""
	}
	if root == nil {
		return ""
	}
	return walk(root, nil)
}

// ResolveCaseBinding validates the selected case flows each have a saved
// version and resolves the immutable binding snapshot (versions, subtree
// roots, expanded case leaves, per-flow resolved sources).
func ResolveCaseBinding(db *gorm.DB, selections []CaseSelection) (*CaseBinding, error) {
	if len(selections) == 0 {
		return nil, errors.New("至少选择一个用例子树")
	}
	b := &CaseBinding{
		Roots:    selections,
		Sources:  []CaseBindingSource{},
		Leaves:   []CaseBindingLeaf{},
		Resolved: map[uint][]ResolvedSource{},
	}
	seen := map[string]bool{}
	for _, sel := range selections {
		cf, err := GetCaseFlow(db, sel.CaseFlowID)
		if err != nil {
			return nil, err
		}
		v, err := resolveCaseVersion(db, sel.CaseFlowID, sel.VersionNo)
		if err != nil {
			return nil, err
		}
		tree, err := parseCaseTree(v.Tree)
		if err != nil {
			return nil, err
		}
		b.Sources = append(b.Sources, CaseBindingSource{CaseFlowID: cf.ID, Name: cf.Name, VersionNo: v.VersionNo})

		var storedSources []model.CaseSource
		_ = json.Unmarshal([]byte(v.Sources), &storedSources)
		resolved, err := resolveCaseSourceRows(db, cf.ID, storedSources)
		if err != nil {
			return nil, err
		}
		b.Resolved[cf.ID] = resolved

		for _, rootID := range sel.RootIDs {
			root := caseflow.Find(tree.Root, rootID)
			if root == nil {
				return nil, fmt.Errorf("用例流 %d 版本 %d 中不存在根节点 %s", cf.ID, v.VersionNo, rootID)
			}
			for _, n := range caseflow.Descendants(root) {
				if len(n.Children) > 0 {
					continue // 只有叶子是用例
				}
				key := fmt.Sprintf("%d:%s", cf.ID, n.ID)
				if seen[key] {
					continue
				}
				seen[key] = true
				b.Leaves = append(b.Leaves, CaseBindingLeaf{
					CaseFlowID: cf.ID, CaseVersionNo: v.VersionNo,
					CaseNodeID: n.ID, Title: n.Title, Path: casePath(tree.Root, n.ID),
					Description: n.Description, Precondition: n.Precondition,
					Input: n.Input, Expected: n.Expected,
				})
			}
		}
	}
	if len(b.Leaves) == 0 {
		return nil, errors.New("选中的子树中没有测试用例叶子")
	}
	return b, nil
}

// SaveFlowCaseBinding persists a binding onto a flow's draft.
func SaveFlowCaseBinding(db *gorm.DB, flowID uint, b *CaseBinding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return db.Model(&model.FlowDraft{}).Where("flow_id = ?", flowID).Update("case_binding", string(raw)).Error
}

// GetFlowCaseBinding reads a flow draft's binding, if any.
func GetFlowCaseBinding(db *gorm.DB, flowID uint) (*CaseBinding, bool, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(d.CaseBinding) == "" {
		return nil, false, nil
	}
	var b CaseBinding
	if err := json.Unmarshal([]byte(d.CaseBinding), &b); err != nil {
		return nil, false, err
	}
	return &b, true, nil
}

// CreateFlowFromCases resolves the case binding first (rejecting case flows
// without a saved version) and only then creates the bound execution flow.
func CreateFlowFromCases(db *gorm.DB, testSetID, userID uint, name, systemPrompt string, selections []CaseSelection) (*model.TestFlow, *CaseBinding, error) {
	b, err := ResolveCaseBinding(db, selections)
	if err != nil {
		return nil, nil, err
	}
	f, err := CreateFlow(db, testSetID, userID, name, systemPrompt)
	if err != nil {
		return nil, nil, err
	}
	if err := SaveFlowCaseBinding(db, f.ID, b); err != nil {
		return nil, nil, err
	}
	return f, b, nil
}

// GenerateFlowFromCases pre-builds the case-unit skeleton from the flow's
// binding, injects the structured case context and runs the fixed generation
// workflow. It reports bound cases left unimplemented.
func GenerateFlowFromCases(ctx context.Context, db *gorm.DB, flowID, userID uint, instruction string, provider ChatProvider, hooks ...AgentHooks) (*AgentResult, error) {
	binding, ok, err := GetFlowCaseBinding(db, flowID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("该执行流没有绑定用例流")
	}
	anchors, err := PrebuildCaseUnits(db, flowID, binding)
	if err != nil {
		return nil, err
	}
	contextMsg := BuildCaseGenerationContext(db, flowID, binding, anchors)
	if strings.TrimSpace(instruction) == "" {
		instruction = "按用例树为每个分支锚点填充执行语义，并保持用例分支自包含。"
	}
	h := AgentHooks{}
	if len(hooks) > 0 {
		h = hooks[0]
	}
	res, err := RunAgent(ctx, db, flowID, userID, AgentOptions{
		Provider:       provider,
		Mode:           ModeGenerate,
		PresetMessages: []openai.Message{{Role: "user", Content: strPtr(contextMsg + "\n\n" + instruction)}},
		Emit:           h.Emit,
	})
	if err != nil {
		return res, err
	}
	// 生成后校验最终覆盖：列出仍未被任何含子节点锚点实现的用例。
	if missing := verifyCaseCoverage(db, flowID, binding); len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, m := range missing {
			names = append(names, m.Title)
		}
		note := "以下用例尚未实现：" + strings.Join(names, "、")
		if res != nil {
			res.Message = strings.TrimSpace(res.Message + " " + note)
		}
		if h.Emit != nil {
			h.Emit(Event{Kind: EventKindCheckpoint, Result: map[string]any{"unimplemented_cases": names}})
		}
	}
	return res, nil
}

// SaveExecutionFlowFromCases validates and saves an enabled version, then
// freezes the case-to-execution mapping and marks the bound cases covered.
// The binding is read from the draft; the whole write is one transaction.
func SaveExecutionFlowFromCases(db *gorm.DB, flowID, userID uint) (*model.FlowVersion, error) {
	if _, ok, err := GetFlowCaseBinding(db, flowID); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("该执行流没有绑定用例流")
	}
	version, res, err := SaveAndEnable(db, flowID, userID)
	if err != nil {
		return nil, err
	}
	if res != nil && res.HasErrors() {
		return nil, ErrFlowValidation
	}
	return version, nil
}
