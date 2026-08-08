package service

import (
	"errors"
	"fmt"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/scheduler"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrFlowNotFound indicates a missing test flow.
var ErrFlowNotFound = errors.New("流不存在")

// ErrFlowValidation indicates a flow failed whole-tree validation.
var ErrFlowValidation = errors.New("流校验失败")

// CreateFlow creates a test flow and its initial empty draft (a start node).
func CreateFlow(db *gorm.DB, testSetID, userID uint, name string) (*model.TestFlow, error) {
	f := &model.TestFlow{TestSetID: testSetID, Name: name, CreatedBy: userID}
	if err := db.Create(f).Error; err != nil {
		return nil, err
	}
	tree := &flow.Tree{
		Start: "n1",
		Nodes: map[string]*flow.Node{
			"n1": flow.NewNode("n1", flow.NodeStart),
		},
	}
	draft := &model.FlowDraft{FlowID: f.ID, Name: name, Tree: tree.String()}
	if err := db.Create(draft).Error; err != nil {
		return nil, err
	}
	return f, nil
}

// GetDraft returns the current working draft of a flow.
func GetDraft(db *gorm.DB, flowID uint) (*model.FlowDraft, error) {
	var d model.FlowDraft
	if err := db.Where("flow_id = ?", flowID).Order("id").First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateDraft replaces the working draft with a new whole-tree snapshot.
func UpdateDraft(db *gorm.DB, flowID uint, name, treeJSON string) (*model.FlowDraft, error) {
	tree, err := flow.ParseTree(treeJSON)
	if err != nil {
		return nil, err
	}
	// Reject structurally broken trees at write time.
	if errs := tree.ValidateTreeShape(); len(errs) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrFlowValidation, errs[0].Message)
	}
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	d.Name = name
	d.Tree = tree.String()
	if err := db.Save(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

// DeleteFlow hard-deletes a flow and all of its associated data: draft,
// versions, execution logs and schedules. Registered cron jobs are cancelled
// after the transaction commits so no orphan schedule keeps firing. sched may
// be nil (e.g. in tests) to skip job cancellation.
func DeleteFlow(db *gorm.DB, sched *ScheduleManager, flowID uint) error {
	var f model.TestFlow
	if err := db.First(&f, flowID).Error; err != nil {
		return ErrFlowNotFound
	}
	// 先收集该流的调度任务，事务提交后统一取消，避免遗留仍在触发的定时任务。
	var schedules []model.FlowSchedule
	if err := db.Where("flow_id = ?", flowID).Find(&schedules).Error; err != nil {
		return err
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{
			&model.FlowDraft{},
			&model.FlowVersion{},
			&model.ExecutionLog{},
			&model.FlowSchedule{},
		} {
			if err := tx.Where("flow_id = ?", flowID).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&f).Error
	})
	if err != nil {
		return err
	}
	if sched != nil {
		for _, s := range schedules {
			if s.JobID == "" {
				continue
			}
			if err := sched.scheduler.Remove(s.JobID); err != nil && !errors.Is(err, scheduler.ErrJobNotFound) {
				log.Warnf("删除流 %d: 取消调度 %d 失败: %v", flowID, s.ID, err)
			}
		}
	}
	return nil
}

// SnapshotAPINodes redundantly snapshots the referenced test units into every
// api node's config so a version is self-contained.
func SnapshotAPINodes(db *gorm.DB, tree *flow.Tree) error {
	for _, n := range tree.Nodes {
		if n == nil || n.Type != flow.NodeAPI {
			continue
		}
		var cfg struct {
			UnitID uint `json:"unit_id"`
		}
		if err := flow.UnmarshalConfig(n, &cfg); err != nil || cfg.UnitID == 0 {
			continue
		}
		var unit model.TestUnit
		if err := db.Unscoped().First(&unit, cfg.UnitID).Error; err != nil {
			continue
		}
		var apiCfg struct {
			UnitID uint           `json:"unit_id"`
			Unit   UnitSnapshot   `json:"unit"`
			Params map[string]any `json:"params,omitempty"`
		}
		_ = flow.UnmarshalConfig(n, &apiCfg)
		apiCfg.UnitID = cfg.UnitID
		apiCfg.Unit = UnitSnapshot{
			ID:          unit.ID,
			Method:      unit.Method,
			Path:        unit.Path,
			Tag:         unit.Tag,
			Name:        unit.Name,
			Params:      unit.Params,
			RequestBody: unit.RequestBody,
			Responses:   unit.Responses,
			Security:    unit.Security,
			Spec:        unit.Spec,
		}
		cfgJSON, err := flow.MarshalConfig(apiCfg)
		if err != nil {
			return err
		}
		n.Config = cfgJSON
	}
	return nil
}

// UnitSnapshot is the redundant interface schema carried inside an api node.
type UnitSnapshot struct {
	ID          uint   `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Params      string `json:"params,omitempty"`
	RequestBody string `json:"request_body,omitempty"`
	Responses   string `json:"responses,omitempty"`
	Security    string `json:"security,omitempty"`
	Spec        string `json:"spec,omitempty"`
}

// SaveAndEnable validates the draft whole tree, then promotes it to a new
// immutable version and clears the previous enabled flag. Api-node schemas are
// snapshotted into the version.
func SaveAndEnable(db *gorm.DB, flowID, userID uint) (*model.FlowVersion, *flow.Result, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, nil, err
	}
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, nil, err
	}
	opts := flow.ValidatorOptions{
		UnitDeleted: func(unitID uint) bool {
			var unit model.TestUnit
			return db.Unscoped().First(&unit, unitID).Error == nil && unit.DeletedAt.Valid
		},
	}
	res := flow.Validate(tree, opts)
	if res.HasErrors() {
		return nil, &res, ErrFlowValidation
	}

	var version *model.FlowVersion
	err = db.Transaction(func(tx *gorm.DB) error {
		// Acquire the write lock before computing the next number so concurrent
		// saves serialize. The enabled-flag clear is the first write statement:
		// under SQLite it upgrades the deferred transaction to a single-writer
		// transaction (losers wait on busy_timeout); on MySQL/Postgres the
		// SELECT ... FOR UPDATE below locks the flow's version rows.
		if err := tx.Model(&model.FlowVersion{}).
			Where("flow_id = ? AND enabled = ?", flowID, true).
			Update("enabled", false).Error; err != nil {
			return err
		}
		var maxNo int
		q := tx.Model(&model.FlowVersion{}).Where("flow_id = ?", flowID)
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		if err := q.Select("COALESCE(MAX(version_no), 0)").Scan(&maxNo).Error; err != nil {
			return err
		}
		version = &model.FlowVersion{
			FlowID:    flowID,
			VersionNo: maxNo + 1,
			Tree:      tree.String(),
			Enabled:   true,
			CreatedBy: userID,
		}
		return tx.Create(version).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return version, &res, nil
}

// ListVersions returns the versions of a flow, newest first.
func ListVersions(db *gorm.DB, flowID uint) ([]model.FlowVersion, error) {
	var versions []model.FlowVersion
	if err := db.Where("flow_id = ?", flowID).Order("version_no desc").Find(&versions).Error; err != nil {
		return nil, err
	}
	return versions, nil
}

// GetVersion returns a version snapshot by number.
func GetVersion(db *gorm.DB, flowID uint, versionNo int) (*model.FlowVersion, error) {
	var v model.FlowVersion
	if err := db.Where("flow_id = ? AND version_no = ?", flowID, versionNo).First(&v).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrFlowNotFound
		}
		return nil, err
	}
	return &v, nil
}
