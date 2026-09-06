package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CaseSelection is one subtree-root selection for execution flow generation.
type CaseSelection struct {
	CaseFlowID uint     `json:"case_flow_id"`
	RootIDs    []string `json:"root_ids"`
}

// CaseGenerationPlan is the resolved snapshot of the selected subtrees.
type CaseGenerationPlan struct {
	CaseFlowIDs  []uint                    `json:"case_flow_ids"`
	NodeIDs      []string                  `json:"node_ids"`
	LeafSnapshot map[string]string         `json:"leaf_snapshot"` // node_id -> title
	Sources      map[uint][]ResolvedSource `json:"sources"`
	Instruction  string                    `json:"instruction"`
}

// BuildCaseGenerationPlan resolves and deduplicates the selected subtrees.
func BuildCaseGenerationPlan(db *gorm.DB, selections []CaseSelection) (*CaseGenerationPlan, error) {
	if len(selections) == 0 {
		return nil, errors.New("至少选择一个用例子树")
	}
	plan := &CaseGenerationPlan{LeafSnapshot: map[string]string{}, Sources: map[uint][]ResolvedSource{}}
	seen := map[string]bool{}
	var lines []string
	for _, sel := range selections {
		view, err := GetCaseTreeView(db, sel.CaseFlowID)
		if err != nil {
			return nil, err
		}
		cf, err := GetCaseFlow(db, sel.CaseFlowID)
		if err != nil {
			return nil, err
		}
		sources, err := ResolveCaseSources(db, sel.CaseFlowID)
		if err != nil {
			return nil, err
		}
		plan.CaseFlowIDs = append(plan.CaseFlowIDs, sel.CaseFlowID)
		plan.Sources[sel.CaseFlowID] = sources
		for _, rootID := range sel.RootIDs {
			root := caseflow.Find(view.Tree.Root, rootID)
			if root == nil {
				return nil, fmt.Errorf("用例流 %d 中不存在根节点 %s", sel.CaseFlowID, rootID)
			}
			for _, n := range caseflow.Descendants(root) {
				key := fmt.Sprintf("%d:%s", sel.CaseFlowID, n.ID)
				if seen[key] {
					continue
				}
				seen[key] = true
				plan.NodeIDs = append(plan.NodeIDs, n.ID)
				plan.LeafSnapshot[n.ID] = n.Title
				lines = append(lines, fmt.Sprintf("[%s] %s", cf.Name, n.Title))
			}
		}
	}
	plan.Instruction = "请根据以下选中的用例子树生成一个执行流：\n" + joinLines(lines)
	return plan, nil
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// GenerateFlowFromCases builds an execution flow request from selected case
// subtrees and delegates to the existing Execution Flow Agent.
func GenerateFlowFromCases(ctx context.Context, db *gorm.DB, flowID, userID uint, selections []CaseSelection, provider ChatProvider, hooks ...AgentHooks) (*AgentResult, error) {
	plan, err := BuildCaseGenerationPlan(db, selections)
	if err != nil {
		return nil, err
	}
	h := AgentHooks{}
	if len(hooks) > 0 {
		h = hooks[0]
	}
	return RunAgent(ctx, db, flowID, userID, AgentOptions{
		Provider: provider, Instruction: plan.Instruction, Mode: ModeGenerate, Emit: h.Emit,
	})
}

// SaveExecutionFlowFromCases validates, snapshots, saves a version, then marks
// selected case nodes covered and creates coverage links in one transaction.
func SaveExecutionFlowFromCases(db *gorm.DB, flowID, userID uint, selections []CaseSelection) (*model.FlowVersion, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	StripOrphanInputs(tree)
	opts := flow.ValidatorOptions{UnitDeleted: func(unitID uint) bool {
		var unit model.TestUnit
		return db.Unscoped().First(&unit, unitID).Error == nil && unit.DeletedAt.Valid
	}}
	res := flow.Validate(tree, opts)
	if res.HasErrors() {
		return nil, ErrFlowValidation
	}
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, err
	}

	plan, err := BuildCaseGenerationPlan(db, selections)
	if err != nil {
		return nil, err
	}

	var version *model.FlowVersion
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.FlowVersion{}).Where("flow_id = ? AND enabled = ?", flowID, true).Update("enabled", false).Error; err != nil {
			return err
		}
		var maxNo int
		if err := tx.Model(&model.FlowVersion{}).Where("flow_id = ?", flowID).Clauses(clause.Locking{Strength: "UPDATE"}).Select("COALESCE(MAX(version_no), 0)").Scan(&maxNo).Error; err != nil {
			return err
		}
		version = &model.FlowVersion{FlowID: flowID, VersionNo: maxNo + 1, Tree: tree.String(), SystemPrompt: d.SystemPrompt, Enabled: true, CreatedBy: userID}
		if err := tx.Create(version).Error; err != nil {
			return err
		}

		snapshot, _ := json.Marshal(plan)
		for _, sel := range selections {
			view, err := GetCaseTreeView(tx, sel.CaseFlowID)
			if err != nil {
				return err
			}
			for _, rootID := range sel.RootIDs {
				root := caseflow.Find(view.Tree.Root, rootID)
				if root == nil {
					return fmt.Errorf("用例根节点不存在: %s", rootID)
				}
				for _, n := range caseflow.Descendants(root) {
					var cn model.CaseNode
					if err := tx.Where("case_flow_id = ? AND node_key = ?", sel.CaseFlowID, n.ID).First(&cn).Error; err != nil {
						return err
					}
					if err := tx.Model(&cn).Update("status", caseflow.StatusCovered).Error; err != nil {
						return err
					}
					var existing model.CaseCoverage
					err := tx.Where("case_node_id = ? AND flow_version_id = ?", cn.ID, version.ID).First(&existing).Error
					if errors.Is(err, gorm.ErrRecordNotFound) {
						if err := tx.Create(&model.CaseCoverage{CaseNodeID: cn.ID, FlowVersionID: version.ID, Snapshot: string(snapshot)}).Error; err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return version, nil
}
