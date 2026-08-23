package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
// systemPrompt is the optional flow-scoped context document.
func CreateFlow(db *gorm.DB, testSetID, userID uint, name, systemPrompt string) (*model.TestFlow, error) {
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
	draft := &model.FlowDraft{FlowID: f.ID, Name: name, Tree: tree.String(), SystemPrompt: systemPrompt}
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

// UpdateDraft replaces the working draft with a new whole-tree snapshot and,
// when systemPrompt is non-nil, the flow-scoped context document. A nil
// systemPrompt leaves the existing document untouched (partial update); an
// explicit empty string clears it. 当 thinking 非空时一并写入深度思考偏好。
func UpdateDraft(db *gorm.DB, flowID uint, name, treeJSON string, systemPrompt *string, thinking string) (*model.FlowDraft, error) {
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
	if systemPrompt != nil {
		d.SystemPrompt = *systemPrompt
	}
	if thinking != "" {
		d.Thinking = thinking
	}
	if err := db.Save(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

// UpdateFlowThinking 更新流的深度思考偏好(thinking 列),不动 tree/文档。
// 仅在用户于对话框中切换深度思考时调用,把偏好持久化到草稿行。
func UpdateFlowThinking(db *gorm.DB, flowID uint, thinking string) error {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return err
	}
	d.Thinking = thinking
	return db.Save(d).Error
}

// FlowThinking 读取流在数据库中持久化的深度思考级别(thinking 列),作为
// agent 提交时的唯一事实来源。空值返回 "disabled"。
func FlowThinking(db *gorm.DB, flowID uint) string {
	d, err := GetDraft(db, flowID)
	if err != nil || d.Thinking == "" {
		return "disabled"
	}
	return d.Thinking
}

// UpdateFlowDoc 更新流的系统提示词文档（system_prompt 列）。
// 仅当 doc 非 nil 时写库：nil 表示不修改（部分更新），显式空串表示清空。
// 只写该列、不触碰 tree——用于 Agent 提交前把前端最新文档落库，
// 使 GenerateFlow/RunAgent 内 GetDraft 实时现读到最新值，消除
// "编辑后未保存即提交读旧值" 的竞态。
func UpdateFlowDoc(db *gorm.DB, flowID uint, doc *string) error {
	if doc == nil {
		return nil
	}
	return db.Model(&model.FlowDraft{}).Where("flow_id = ?", flowID).Update("system_prompt", *doc).Error
}

// StripOrphanInputs 移除所有 Source 为空的 input。
// 这些 input 在执行中无实际作用（resolveInputs 解析不到就跳过），
// 仅会产生校验假阳性（contract.input_unresolved）。
// 有 source 的 input（如 auth 类 $cache.token）不受影响。
func StripOrphanInputs(tree *flow.Tree) {
	for _, n := range tree.Nodes {
		if n == nil || len(n.Inputs) == 0 {
			continue
		}
		for k, v := range n.Inputs {
			if v.Source == "" {
				delete(n.Inputs, k)
			}
		}
	}
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

// DuplicateFlow 复制一个测试流及其当前草稿到同一测试集下的新流。
// name 为空时自动生成为 `{原名} 副本`。复制只携带当前工作状态（草稿树），
// 不复制版本快照、运行记录、定时调度与 Agent 会话。
func DuplicateFlow(db *gorm.DB, flowID, userID uint, name string) (*model.TestFlow, error) {
	var f model.TestFlow
	if err := db.First(&f, flowID).Error; err != nil {
		return nil, ErrFlowNotFound
	}
	newName := strings.TrimSpace(name)
	if newName == "" {
		newName = f.Name + " 副本"
	}
	var dup *model.TestFlow
	err := db.Transaction(func(tx *gorm.DB) error {
		nd := &model.TestFlow{TestSetID: f.TestSetID, Name: newName, CreatedBy: userID}
		if err := tx.Create(nd).Error; err != nil {
			return err
		}
		treeJSON := ""
		systemPrompt := ""
		var d model.FlowDraft
		if err := tx.Where("flow_id = ?", flowID).Order("id").First(&d).Error; err == nil {
			treeJSON = d.Tree
			systemPrompt = d.SystemPrompt
		}
		draft := &model.FlowDraft{FlowID: nd.ID, Name: newName, Tree: treeJSON, SystemPrompt: systemPrompt}
		if err := tx.Create(draft).Error; err != nil {
			return err
		}
		dup = nd
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dup, nil
}

// RenameFlow 重命名测试流,同步更新流记录与草稿记录中的名称,
// 保证列表展示与编辑器标题一致。draft 不存在时仅更新流记录。
func RenameFlow(db *gorm.DB, flowID uint, name string) (*model.TestFlow, error) {
	var f model.TestFlow
	if err := db.First(&f, flowID).Error; err != nil {
		return nil, ErrFlowNotFound
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.TestFlow{}).Where("id = ?", flowID).Update("name", name).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.FlowDraft{}).Where("flow_id = ?", flowID).Update("name", name).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	f.Name = name
	return &f, nil
}

// swaggerParam is one parameter entry parsed from a unit's Params JSON array
// (OpenAPI 2.0 format).
type swaggerParam struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// swaggerSchema is a JSON Schema subset extracted from a request_body.
type swaggerSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]swaggerSchema `json:"properties"`
	Required   []string                 `json:"required,omitempty"`
}

// deriveInputs parses a test unit's swagger params and request_body strings,
// producing I/O key declarations. Auth-related keys (matched by isAuthKey)
// are pre-wired to "$cache.token".
func deriveInputs(unit model.TestUnit) map[string]flow.IOKey {
	io := map[string]flow.IOKey{}

	// 解析 params JSON 数组
	if strings.TrimSpace(unit.Params) != "" && unit.Params != "null" {
		var params []swaggerParam
		if err := json.Unmarshal([]byte(unit.Params), &params); err != nil {
			log.Warnf("deriveInputs: 解析 unit %d params 失败: %v", unit.ID, err)
		} else {
			for _, p := range params {
				key := swaggerTypeToIO(p.Type)
				src := ""
				if isAuthKey(p.Name) {
					src = "$cache.token"
				}
				io[p.Name] = flow.IOKey{Type: key, Source: src, In: p.In}
			}
		}
	}

	// 解析 request_body JSON schema
	if strings.TrimSpace(unit.RequestBody) != "" && unit.RequestBody != "null" {
		var schema swaggerSchema
		if err := json.Unmarshal([]byte(unit.RequestBody), &schema); err != nil {
			log.Warnf("deriveInputs: 解析 unit %d request_body 失败: %v", unit.ID, err)
		} else {
			for propName, prop := range schema.Properties {
				// 跳过已存在的 key（params 优先）
				if _, exists := io[propName]; exists {
					continue
				}
				key := swaggerTypeToIO(prop.Type)
				src := ""
				if isAuthKey(propName) {
					src = "$cache.token"
				}
				io[propName] = flow.IOKey{Type: key, Source: src, In: "body"}
			}
		}
	}

	return io
}

// swaggerTypeToIO maps a Swagger/OpenAPI type string to a flow IOType.
func swaggerTypeToIO(t string) flow.IOType {
	switch t {
	case "object":
		return flow.IOTypeObject
	case "array":
		return flow.IOTypeArray
	default:
		return flow.IOTypePrimitive
	}
}

// SnapshotAPINodes redundantly snapshots the referenced test units into every
// api node's config so a version is self-contained. It also auto-populates the
// node's I/O contract (inputs) from the unit's swagger parameter definitions.
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
			UnitID  uint           `json:"unit_id"`
			Unit    UnitSnapshot   `json:"unit"`
			Params  map[string]any `json:"params,omitempty"`
			Headers map[string]any `json:"headers,omitempty"`
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

		// 自动填充 I/O 契约：从 Swagger 参数定义推导 inputs。
		// 仅注入带有 source 的 input（如 auth 类 → $cache.token），
		// 无 source 的 input（如 request_body 的 body）不注入，避免校验假阳性。
		derived := deriveInputs(unit)
		if len(derived) > 0 {
			for k, v := range derived {
				if v.Source == "" {
					continue
				}
				if _, exists := n.Inputs[k]; !exists {
					if len(n.Inputs) == 0 {
						n.Inputs = map[string]flow.IOKey{}
					}
					n.Inputs[k] = v
				}
			}
		}
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
	// 清理旧版本恢复带入的无 source input（如 body），避免校验假阳性。
	StripOrphanInputs(tree)
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

	// 校验通过后再快照单元元数据，避免自动注入的 inputs 干扰校验结果。
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, nil, err
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
			FlowID:       flowID,
			VersionNo:    maxNo + 1,
			Tree:         tree.String(),
			SystemPrompt: d.SystemPrompt,
			Enabled:      true,
			CreatedBy:    userID,
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
