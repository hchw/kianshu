package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Case Flow 领域错误。
var (
	ErrCaseFlowNotFound   = errors.New("用例流不存在")
	ErrCaseSourceRequired = errors.New("至少需要一个背景文档或接口范围来源")
	ErrCaseTreeValidation = errors.New("用例树校验失败")
	ErrCaseSourceConflict = errors.New("来源引用冲突")
)

// SourceInput 描述一次来源绑定请求。
type SourceInput struct {
	Kind       string   `json:"kind"` // document | all | tag
	DocumentID uint     `json:"document_id,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// ResolvedSource 是生成时实际使用的来源快照。
type ResolvedSource struct {
	Kind        string                    `json:"kind"`
	DocumentID  uint                      `json:"document_id,omitempty"`
	Document    *model.BackgroundDocument `json:"document,omitempty"`
	Tags        []string                  `json:"tags,omitempty"`
	UnitIDs     []uint                    `json:"unit_ids,omitempty"`
	Unavailable bool                      `json:"unavailable,omitempty"`
}

func genNodeID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "c" + hex.EncodeToString(b)
}

func emptyCaseTree() string {
	tree := caseflow.Tree{Root: &caseflow.Node{ID: genNodeID(), Title: "用例根", Status: caseflow.StatusUncovered}}
	return treeJSON(tree)
}

func treeJSON(t caseflow.Tree) string {
	b, _ := json.Marshal(t)
	return string(b)
}

func parseCaseTree(raw string) (caseflow.Tree, error) {
	var t caseflow.Tree
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return t, err
	}
	if err := caseflow.Validate(t); err != nil {
		return t, fmt.Errorf("%w: %v", ErrCaseTreeValidation, err)
	}
	return t, nil
}

func getCaseFlowDraft(db *gorm.DB, caseFlowID uint) (*model.CaseFlowDraft, error) {
	var d model.CaseFlowDraft
	if err := db.Where("case_flow_id = ?", caseFlowID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCaseFlowNotFound
		}
		return nil, err
	}
	return &d, nil
}

func validateSources(inputs []SourceInput) error {
	if len(inputs) == 0 {
		return ErrCaseSourceRequired
	}
	hasUsable := false
	for _, in := range inputs {
		switch in.Kind {
		case "document":
			if in.DocumentID != 0 {
				hasUsable = true
			}
		case "all":
			hasUsable = true
		case "tag":
			if len(in.Tags) > 0 {
				hasUsable = true
			}
		}
	}
	if !hasUsable {
		return ErrCaseSourceRequired
	}
	return nil
}

// CreateCaseFlow 创建用例流、空草稿和来源绑定。
func CreateCaseFlow(db *gorm.DB, testSetID, userID uint, name string, sources []SourceInput) (*model.CaseFlow, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("名称不能为空")
	}
	if err := validateSources(sources); err != nil {
		return nil, err
	}
	var cf *model.CaseFlow
	err := db.Transaction(func(tx *gorm.DB) error {
		cf = &model.CaseFlow{TestSetID: testSetID, Name: name, CreatedBy: userID}
		if err := tx.Create(cf).Error; err != nil {
			return err
		}
		draft := &model.CaseFlowDraft{CaseFlowID: cf.ID, Tree: emptyCaseTree(), Revision: 1}
		if err := tx.Create(draft).Error; err != nil {
			return err
		}
		empty, _ := parseCaseTree(draft.Tree)
		if err := syncCaseNodes(tx, cf.ID, empty); err != nil {
			return err
		}
		return replaceSources(tx, cf.ID, sources)
	})
	if err != nil {
		return nil, err
	}
	return cf, nil
}

func replaceSources(db *gorm.DB, caseFlowID uint, sources []SourceInput) error {
	if err := db.Where("case_flow_id = ?", caseFlowID).Delete(&model.CaseSource{}).Error; err != nil {
		return err
	}
	for _, in := range sources {
		scope, _ := json.Marshal(in.Tags)
		s := &model.CaseSource{CaseFlowID: caseFlowID, Kind: in.Kind, DocumentID: in.DocumentID, Scope: string(scope)}
		if err := db.Create(s).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetCaseFlow returns the case flow metadata.
func GetCaseFlow(db *gorm.DB, caseFlowID uint) (*model.CaseFlow, error) {
	var cf model.CaseFlow
	if err := db.First(&cf, caseFlowID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCaseFlowNotFound
		}
		return nil, err
	}
	return &cf, nil
}

// ListCaseFlows lists case flows in a test set.
func ListCaseFlows(db *gorm.DB, testSetID uint) ([]model.CaseFlow, error) {
	var out []model.CaseFlow
	if err := db.Where("test_set_id = ?", testSetID).Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// RenameCaseFlow 重命名用例流。
func RenameCaseFlow(db *gorm.DB, caseFlowID uint, name string) (*model.CaseFlow, error) {
	if name == "" {
		return nil, errors.New("名称不能为空")
	}
	var cf model.CaseFlow
	if err := db.First(&cf, caseFlowID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCaseFlowNotFound
		}
		return nil, err
	}
	cf.Name = name
	if err := db.Save(&cf).Error; err != nil {
		return nil, err
	}
	return &cf, nil
}

// DeleteCaseFlow removes a case flow and all owned data.
func DeleteCaseFlow(db *gorm.DB, caseFlowID uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var cf model.CaseFlow
		if err := tx.First(&cf, caseFlowID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaseFlowNotFound
			}
			return err
		}
		var nodes []model.CaseNode
		if err := tx.Where("case_flow_id = ?", caseFlowID).Find(&nodes).Error; err != nil {
			return err
		}
		nodeIDs := make([]uint, 0, len(nodes))
		for _, n := range nodes {
			nodeIDs = append(nodeIDs, n.ID)
		}
		for _, m := range []any{
			&model.CaseFlowDraft{}, &model.CaseFlowVersion{}, &model.CaseSource{},
			&model.CaseNode{}, &model.CaseFlowSession{},
		} {
			if err := tx.Where("case_flow_id = ?", caseFlowID).Delete(m).Error; err != nil {
				return err
			}
		}
		// CaseCoverage 通过 case_node_id 间接关联到用例流，需按节点 ID 删除。
		if len(nodeIDs) > 0 {
			if err := tx.Where("case_node_id IN ?", nodeIDs).Delete(&model.CaseCoverage{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&cf).Error
	})
}

// UpdateCaseFlowDraft 校验并写入新的用例树草稿，使用 revision 做并发保护。
func UpdateCaseFlowDraft(db *gorm.DB, caseFlowID uint, expectedRevision uint, treeJSON string) (*model.CaseFlowDraft, error) {
	tree, err := parseCaseTree(treeJSON)
	if err != nil {
		return nil, err
	}
	var d model.CaseFlowDraft
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("case_flow_id = ?", caseFlowID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaseFlowNotFound
			}
			return err
		}
		if d.Revision != expectedRevision {
			return errors.New("草稿已被他人修改,请刷新后重试")
		}
		d.Revision++
		d.Tree = treeJSON
		if err := tx.Save(&d).Error; err != nil {
			return err
		}
		return syncCaseNodes(tx, caseFlowID, tree)
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func syncCaseNodes(db *gorm.DB, caseFlowID uint, tree caseflow.Tree) error {
	nodes := caseflow.Descendants(tree.Root)
	for _, n := range nodes {
		var existing model.CaseNode
		err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, n.ID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status := n.Status
			if status == "" {
				status = caseflow.StatusUncovered
			}
			if err := db.Create(&model.CaseNode{CaseFlowID: caseFlowID, NodeKey: n.ID, Status: status}).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing.Status != n.Status {
			if err := db.Model(&existing).Update("status", n.Status).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// SaveCaseFlowVersion 保存不可变版本快照。
func SaveCaseFlowVersion(db *gorm.DB, caseFlowID, userID uint) (*model.CaseFlowVersion, error) {
	d, err := getCaseFlowDraft(db, caseFlowID)
	if err != nil {
		return nil, err
	}
	if _, err := parseCaseTree(d.Tree); err != nil {
		return nil, err
	}
	var sources []model.CaseSource
	if err := db.Where("case_flow_id = ?", caseFlowID).Find(&sources).Error; err != nil {
		return nil, err
	}
	srcJSON, _ := json.Marshal(sources)
	var version *model.CaseFlowVersion
	err = db.Transaction(func(tx *gorm.DB) error {
		var maxNo int
		if err := tx.Model(&model.CaseFlowVersion{}).Where("case_flow_id = ?", caseFlowID).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("COALESCE(MAX(version_no), 0)").Scan(&maxNo).Error; err != nil {
			return err
		}
		version = &model.CaseFlowVersion{CaseFlowID: caseFlowID, VersionNo: maxNo + 1, Tree: d.Tree, Sources: string(srcJSON), CreatedBy: userID}
		return tx.Create(version).Error
	})
	if err != nil {
		return nil, err
	}
	return version, nil
}

// ListCaseFlowVersions returns versions newest first.
func ListCaseFlowVersions(db *gorm.DB, caseFlowID uint) ([]model.CaseFlowVersion, error) {
	var out []model.CaseFlowVersion
	if err := db.Where("case_flow_id = ?", caseFlowID).Order("version_no desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// RestoreCaseFlowVersion copies a version tree into the draft without mutating the version.
func RestoreCaseFlowVersion(db *gorm.DB, caseFlowID uint, versionNo int) (*model.CaseFlowDraft, error) {
	var v model.CaseFlowVersion
	if err := db.Where("case_flow_id = ? AND version_no = ?", caseFlowID, versionNo).First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCaseFlowNotFound
		}
		return nil, err
	}
	if _, err := parseCaseTree(v.Tree); err != nil {
		return nil, err
	}
	var d model.CaseFlowDraft
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("case_flow_id = ?", caseFlowID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaseFlowNotFound
			}
			return err
		}
		d.Revision++
		d.Tree = v.Tree
		if err := tx.Save(&d).Error; err != nil {
			return err
		}
		tree, _ := parseCaseTree(v.Tree)
		return syncCaseNodes(tx, caseFlowID, tree)
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// AddCaseSource adds a source; nil removes the final valid source protection.
func AddCaseSource(db *gorm.DB, caseFlowID uint, in SourceInput) error {
	if err := validateSources([]SourceInput{in}); err != nil {
		return err
	}
	scope, _ := json.Marshal(in.Tags)
	return db.Create(&model.CaseSource{CaseFlowID: caseFlowID, Kind: in.Kind, DocumentID: in.DocumentID, Scope: string(scope)}).Error
}

// RemoveCaseSource removes one source, rejecting removal of the final source.
func RemoveCaseSource(db *gorm.DB, caseFlowID, sourceID uint) error {
	var count int64
	if err := db.Model(&model.CaseSource{}).Where("case_flow_id = ?", caseFlowID).Count(&count).Error; err != nil {
		return err
	}
	if count <= 1 {
		return ErrCaseSourceRequired
	}
	return db.Where("id = ? AND case_flow_id = ?", sourceID, caseFlowID).Delete(&model.CaseSource{}).Error
}

// ListCaseSources returns the current bindings.
func ListCaseSources(db *gorm.DB, caseFlowID uint) ([]model.CaseSource, error) {
	var out []model.CaseSource
	if err := db.Where("case_flow_id = ?", caseFlowID).Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveCaseSources 把来源绑定解析为生成时实际使用的内容快照。
func ResolveCaseSources(db *gorm.DB, caseFlowID uint) ([]ResolvedSource, error) {
	sources, err := ListCaseSources(db, caseFlowID)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedSource, 0, len(sources))
	for _, s := range sources {
		switch s.Kind {
		case "document":
			var doc model.BackgroundDocument
			if err := db.First(&doc, s.DocumentID).Error; err != nil {
				out = append(out, ResolvedSource{Kind: s.Kind, DocumentID: s.DocumentID, Unavailable: true})
				continue
			}
			out = append(out, ResolvedSource{Kind: s.Kind, DocumentID: doc.ID, Document: &doc})
		case "all":
			var ids []uint
			if err := db.Model(&model.TestUnit{}).Where("test_set_id = (SELECT test_set_id FROM case_flows WHERE id = ?)", caseFlowID).Pluck("id", &ids).Error; err != nil {
				return nil, err
			}
			out = append(out, ResolvedSource{Kind: s.Kind, UnitIDs: ids})
		case "tag":
			var tags []string
			_ = json.Unmarshal([]byte(s.Scope), &tags)
			var ids []uint
			if err := db.Model(&model.TestUnit{}).Where("test_set_id = (SELECT test_set_id FROM case_flows WHERE id = ?) AND tag IN ?", caseFlowID, tags).Pluck("id", &ids).Error; err != nil {
				return nil, err
			}
			out = append(out, ResolvedSource{Kind: s.Kind, Tags: tags, UnitIDs: ids})
		}
	}
	return out, nil
}

// ---- Case Tree 节点操作 ----

type nodeMutation struct {
	expectedRevision uint
	apply            func(*caseflow.Tree) error
}

func mutateCaseTree(db *gorm.DB, caseFlowID uint, m nodeMutation) (*model.CaseFlowDraft, error) {
	d, err := getCaseFlowDraft(db, caseFlowID)
	if err != nil {
		return nil, err
	}
	tree, err := parseCaseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	if err := m.apply(&tree); err != nil {
		return nil, err
	}
	if err := caseflow.Validate(tree); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCaseTreeValidation, err)
	}
	return UpdateCaseFlowDraft(db, caseFlowID, m.expectedRevision, treeJSON(tree))
}

// AddCaseNode appends a child under a parent.
func AddCaseNode(db *gorm.DB, caseFlowID uint, expectedRevision uint, parentID, title string) (*model.CaseFlowDraft, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("用例标题不能为空")
	}
	return mutateCaseTree(db, caseFlowID, nodeMutation{expectedRevision: expectedRevision, apply: func(t *caseflow.Tree) error {
		parent := caseflow.Find(t.Root, parentID)
		if parent == nil {
			return errors.New("父节点不存在")
		}
		parent.Children = append(parent.Children, &caseflow.Node{ID: genNodeID(), Title: title, Status: caseflow.StatusUncovered})
		return nil
	}})
}

// CaseNodeUpdate 是节点详情和画布坐标的可选更新字段。
type CaseNodeUpdate struct {
	Title        *string
	Description  *string
	Precondition *string
	Input        *string
	Expected     *string
	X            *float64
	Y            *float64
}

// UpdateCaseNode 更新节点详情和画布坐标。
func UpdateCaseNode(db *gorm.DB, caseFlowID uint, expectedRevision uint, nodeID string, update CaseNodeUpdate) (*model.CaseFlowDraft, error) {
	if update.Title != nil && strings.TrimSpace(*update.Title) == "" {
		return nil, errors.New("用例标题不能为空")
	}
	return mutateCaseTree(db, caseFlowID, nodeMutation{expectedRevision: expectedRevision, apply: func(t *caseflow.Tree) error {
		n := caseflow.Find(t.Root, nodeID)
		if n == nil {
			return errors.New("节点不存在")
		}
		if update.Title != nil {
			n.Title = *update.Title
		}
		if update.Description != nil {
			n.Description = *update.Description
		}
		if update.Precondition != nil {
			n.Precondition = *update.Precondition
		}
		if update.Input != nil {
			n.Input = *update.Input
		}
		if update.Expected != nil {
			n.Expected = *update.Expected
		}
		if update.X != nil {
			n.X = update.X
		}
		if update.Y != nil {
			n.Y = update.Y
		}
		return nil
	}})
}

// MoveCaseNode moves a node under a new parent.
func MoveCaseNode(db *gorm.DB, caseFlowID uint, expectedRevision uint, nodeID, newParentID string) (*model.CaseFlowDraft, error) {
	if nodeID == newParentID {
		return nil, errors.New("不能移动到自身")
	}
	return mutateCaseTree(db, caseFlowID, nodeMutation{expectedRevision: expectedRevision, apply: func(t *caseflow.Tree) error {
		if newParentID != "" {
			if caseflow.Find(t.Root, newParentID) == nil {
				return errors.New("目标父节点不存在")
			}
		}
		node, ok := caseflow.RemoveFrom(t.Root, nodeID)
		if !ok {
			return errors.New("节点不存在")
		}
		if newParentID == "" {
			t.Root = node
			return nil
		}
		parent := caseflow.Find(t.Root, newParentID)
		parent.Children = append(parent.Children, node)
		return nil
	}})
}

// DeleteCaseNode removes a subtree; the root itself cannot be deleted.
func DeleteCaseNode(db *gorm.DB, caseFlowID uint, expectedRevision uint, nodeID string) (*model.CaseFlowDraft, error) {
	return mutateCaseTree(db, caseFlowID, nodeMutation{expectedRevision: expectedRevision, apply: func(t *caseflow.Tree) error {
		if t.Root.ID == nodeID {
			return errors.New("根节点不可删除")
		}
		if _, ok := caseflow.RemoveFrom(t.Root, nodeID); !ok {
			return errors.New("节点不存在")
		}
		return nil
	}})
}

// SetCaseStatus sets a status on a node and all descendants.
func SetCaseStatus(db *gorm.DB, caseFlowID uint, expectedRevision uint, nodeID, status string) (*model.CaseFlowDraft, error) {
	if status != caseflow.StatusCovered && status != caseflow.StatusUncovered {
		return nil, errors.New("无效的用例状态")
	}
	return mutateCaseTree(db, caseFlowID, nodeMutation{expectedRevision: expectedRevision, apply: func(t *caseflow.Tree) error {
		n := caseflow.Find(t.Root, nodeID)
		if n == nil {
			return errors.New("节点不存在")
		}
		for _, d := range caseflow.Descendants(n) {
			d.Status = status
		}
		return nil
	}})
}

// CaseTreeView is the tree plus stable node registry for the editor/API.
type CaseTreeView struct {
	Draft *model.CaseFlowDraft `json:"draft"`
	Tree  caseflow.Tree        `json:"tree"`
}

// GetCaseTreeView returns the parsed draft tree.
func GetCaseTreeView(db *gorm.DB, caseFlowID uint) (*CaseTreeView, error) {
	d, err := getCaseFlowDraft(db, caseFlowID)
	if err != nil {
		return nil, err
	}
	tree, err := parseCaseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	return &CaseTreeView{Draft: d, Tree: tree}, nil
}

// AssociateCaseFlowVersion links case nodes to an execution flow version.
func AssociateCaseFlowVersion(db *gorm.DB, caseFlowID uint, flowVersionID uint, nodeIDs []string, snapshot string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, id := range nodeIDs {
			var n model.CaseNode
			if err := tx.Where("case_flow_id = ? AND node_key = ?", caseFlowID, id).First(&n).Error; err != nil {
				return err
			}
			var existing model.CaseCoverage
			err := tx.Where("case_node_id = ? AND flow_version_id = ?", n.ID, flowVersionID).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&model.CaseCoverage{CaseNodeID: n.ID, FlowVersionID: flowVersionID, Snapshot: snapshot}).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		return nil
	})
}

// ListCaseNodeFlows returns associated execution flow versions for a node.
func ListCaseNodeFlows(db *gorm.DB, caseFlowID uint, nodeID string) ([]model.FlowVersion, error) {
	var n model.CaseNode
	if err := db.Where("case_flow_id = ? AND node_key = ?", caseFlowID, nodeID).First(&n).Error; err != nil {
		return nil, err
	}
	var versions []model.FlowVersion
	err := db.Model(&model.FlowVersion{}).
		Joins("JOIN case_coverages ON case_coverages.flow_version_id = flow_versions.id").
		Where("case_coverages.case_node_id = ?", n.ID).
		Order("flow_versions.version_no desc").
		Find(&versions).Error
	return versions, err
}
