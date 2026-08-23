package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// toolNames enumerates the agent tool set exposed to the LLM.
const (
	toolListUnits   = "list_units"
	toolFilterUnits = "filter_units"
	toolGetFlow     = "get_flow"
	toolCreateNode  = "create_node"
	toolUpdateNode  = "update_node"
	toolDeleteNode  = "delete_node"
	toolLinkNodes   = "link_nodes"
	toolValidate    = "validate_flow"
	toolCheckpoint  = "checkpoint"
)

// UnitBrief is the summarized unit shape fed to the LLM.
type UnitBrief struct {
	ID       uint   `json:"id"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Slug     string `json:"slug"`
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	Security string `json:"security,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

// ToolContext carries the state tools mutate: the in-memory working tree, the
// DB for unit lookups, the test set id, and the selected-node scope.
type ToolContext struct {
	DB        *gorm.DB
	TestSetID uint
	Tree      *flow.Tree
	Scope     map[string]bool
}

// ToolResult is the structured outcome of one tool call, fed back to the LLM.
type ToolResult struct {
	OK             bool            `json:"ok"`
	Data           any             `json:"data,omitempty"`
	Error          string          `json:"error,omitempty"`
	ExpectedFormat string          `json:"expected_format,omitempty"`
	Paused         bool            `json:"paused,omitempty"`
	Questions      []PauseQuestion `json:"questions,omitempty"`
}

// toolError builds a failed ToolResult with a corrected-format hint.
func toolError(expected, msgFormat string, args ...any) *ToolResult {
	return &ToolResult{OK: false, Error: fmt.Sprintf(msgFormat, args...), ExpectedFormat: expected}
}

// ExecTool dispatches one agent tool call against the working tree.
func ExecTool(ctx *ToolContext, name string, args json.RawMessage) *ToolResult {
	switch name {
	case toolListUnits, toolFilterUnits:
		return execListUnits(ctx.DB, ctx.TestSetID, args)
	case toolGetFlow:
		return &ToolResult{OK: true, Data: ctx.Tree}
	case toolCreateNode:
		return execCreateNode(ctx, args)
	case toolUpdateNode:
		return execUpdateNode(ctx, args)
	case toolDeleteNode:
		return execDeleteNode(ctx, args)
	case toolLinkNodes:
		return execLinkNodes(ctx, args)
	case toolValidate:
		return execValidateFlow(ctx)
	case toolCheckpoint:
		return execValidateFlow(ctx)
	default:
		return toolError("", "工具不存在,可用工具: %s", strings.Join(allToolNames(), ", "))
	}
}

func allToolNames() []string {
	return []string{toolListUnits, toolFilterUnits, toolGetFlow, toolCreateNode, toolUpdateNode, toolDeleteNode, toolLinkNodes, toolValidate, toolCheckpoint}
}

// listUnitsQuery filters non-deleted test units of the test set.
func listUnitsQuery(db *gorm.DB, testSetID uint, tag, q string) ([]model.TestUnit, error) {
	query := db.Model(&model.TestUnit{}).Where("test_set_id = ? AND deleted_at IS NULL", testSetID)
	if tag != "" {
		query = query.Where("tag = ?", tag)
	}
	if kw := strings.TrimSpace(q); kw != "" {
		like := "%" + kw + "%"
		query = query.Where("name LIKE ? OR path LIKE ? OR slug LIKE ?", like, like, like)
	}
	var units []model.TestUnit
	if err := query.Order("slug").Limit(500).Find(&units).Error; err != nil {
		return nil, err
	}
	return units, nil
}

func toBriefs(units []model.TestUnit) []UnitBrief {
	out := make([]UnitBrief, 0, len(units))
	for _, u := range units {
		out = append(out, UnitBrief{
			ID: u.ID, Method: u.Method, Path: u.Path, Slug: u.Slug,
			Tag: u.Tag, Name: u.Name, Security: u.Security, Auth: securityScheme(u.Security),
		})
	}
	return out
}

// filterArgs matches both list_units and filter_units argument shapes.
type filterArgs struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	Path string `json:"path"`
	Q    string `json:"q"`
}

func execListUnits(db *gorm.DB, testSetID uint, raw json.RawMessage) *ToolResult {
	var a filterArgs
	if len(raw) > 0 && string(raw) != "{}" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return toolError(`{"tag":"","name":"","path":"","q":""}`, "参数格式错误: %v", err)
		}
	}
	q := firstNonEmpty(a.Q, a.Name, a.Path)
	units, err := listUnitsQuery(db, testSetID, a.Tag, q)
	if err != nil {
		return toolError("{}", "查询单元失败: %v", err)
	}
	return &ToolResult{OK: true, Data: map[string]any{"count": len(units), "units": toBriefs(units)}}
}

// createNodeArgs is the create_node argument shape.
type createNodeArgs struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Parent  string                `json:"parent"`
	Inputs  map[string]flow.IOKey `json:"inputs"`
	Outputs map[string]flow.IOKey `json:"outputs"`
	Config  json.RawMessage       `json:"config"`
}

func execCreateNode(ctx *ToolContext, raw json.RawMessage) *ToolResult {
	var a createNodeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolError(`{"id":"nX","type":"api|assert|loop|try|catch|cache-set|adapter","parent":"nY","inputs":{},"outputs":{},"config":{}}`, "参数格式错误: %v", err)
	}
	if a.ID == "" {
		return toolError(`{"id":"nX","type":"...","config":{}}`, "节点 id 必填")
	}
	if _, exists := ctx.Tree.Nodes[a.ID]; exists {
		return toolError("请换用新的节点 id", "节点 %s 已存在", a.ID)
	}
	typ := flow.NodeType(a.Type)
	switch typ {
	case flow.NodeStart, flow.NodeAPI, flow.NodeAssert, flow.NodeLoop,
		flow.NodeTry, flow.NodeCatch, flow.NodeAdapter:
	case flow.NodeCacheSet:
	default:
		return toolError("节点类型: start|api|assert|loop|try|catch|cache-set|adapter", "非法节点类型: %s", a.Type)
	}
	if typ == flow.NodeStart {
		return toolError("start 根节点已存在,勿重复创建", "start 根节点已存在")
	}
	node := &flow.Node{
		ID: a.ID, Type: typ, Inputs: a.Inputs, Outputs: a.Outputs, Config: a.Config,
	}
	if err := validateAPIBodyShape(ctx.DB, node); err != nil {
		return toolError(`{"inputs":{"<字段>":{"type":"primitive","source":"<来源>","in":"body"}},"config":{"unit_id":<number>,"params":{}}}`, "%v", err)
	}
	ctx.Tree.Nodes[a.ID] = node
	if a.Parent != "" {
		if _, ok := ctx.Tree.Nodes[a.Parent]; !ok {
			return toolError("parent 必须是已存在节点的 id", "父节点不存在: %s", a.Parent)
		}
		ctx.Tree.AddChild(a.Parent, a.ID)
	}
	if ctx.Scope != nil {
		ctx.Scope[a.ID] = true
	}
	return &ToolResult{OK: true, Data: node}
}

// updateNodeArgs matches update_node.
type updateNodeArgs struct {
	ID      string                `json:"id"`
	Inputs  map[string]flow.IOKey `json:"inputs"`
	Outputs map[string]flow.IOKey `json:"outputs"`
	Config  json.RawMessage       `json:"config"`
}

func execUpdateNode(ctx *ToolContext, raw json.RawMessage) *ToolResult {
	var a updateNodeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolError(`{"id":"nX","inputs":{},"outputs":{},"config":{}}`, "参数格式错误: %v", err)
	}
	node, ok := ctx.Tree.Nodes[a.ID]
	if !ok {
		return toolError("先 create_node 或从 get_flow 读取现有 id", "节点不存在: %s", a.ID)
	}
	if !inScope(ctx, a.ID) {
		nodeType := string(node.Type)
		toolArgs, _ := json.Marshal(a)
		return &ToolResult{
			Paused: true,
			Questions: []PauseQuestion{{
				ID:        "scope-" + a.ID,
				Type:      "scope",
				Question:  fmt.Sprintf("Agent 想修改节点 %s(%s)，但该节点不在你勾选的修改范围内。是否允许？", a.ID, nodeType),
				Options:   []string{"允许", "拒绝", "允许本次全部越界节点"},
				NodeID:    a.ID,
				NodeType:  nodeType,
				Operation: toolUpdateNode,
				ToolArgs:  string(toolArgs),
			}},
		}
	}
	// 在写入前校验 API body 结构，避免错误节点进入工作树。
	candidate := *node
	if a.Inputs != nil {
		candidate.Inputs = a.Inputs
	}
	if a.Config != nil {
		candidate.Config = a.Config
	}
	if err := validateAPIBodyShape(ctx.DB, &candidate); err != nil {
		return toolError(`{"inputs":{"<字段>":{"type":"primitive","source":"<来源>","in":"body"}},"config":{"unit_id":<number>,"params":{}}}`, "%v", err)
	}
	if a.Inputs != nil {
		node.Inputs = a.Inputs
	}
	if a.Outputs != nil {
		node.Outputs = a.Outputs
	}
	if len(a.Config) > 0 {
		node.Config = a.Config
	}
	return &ToolResult{OK: true, Data: node}
}

func execDeleteNode(ctx *ToolContext, raw json.RawMessage) *ToolResult {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.ID == "" {
		return toolError(`{"id":"nX"}`, "参数格式错误: 缺少节点 id")
	}
	node, ok := ctx.Tree.Nodes[a.ID]
	if !ok {
		return toolError("从 get_flow 读取现有节点 id", "节点不存在: %s", a.ID)
	}
	if a.ID == ctx.Tree.Start {
		return toolError("", "不能删除 start 根节点")
	}
	if !inScope(ctx, a.ID) {
		nodeType := string(node.Type)
		toolArgs, _ := json.Marshal(a)
		return &ToolResult{
			Paused: true,
			Questions: []PauseQuestion{{
				ID:        "scope-" + a.ID,
				Type:      "scope",
				Question:  fmt.Sprintf("Agent 想删除节点 %s(%s)，但该节点不在你勾选的修改范围内。是否允许？", a.ID, nodeType),
				Options:   []string{"允许", "拒绝", "允许本次全部越界节点"},
				NodeID:    a.ID,
				NodeType:  nodeType,
				Operation: toolDeleteNode,
				ToolArgs:  string(toolArgs),
			}},
		}
	}
	deleteSubtree(ctx.Tree, node, ctx.Scope)
	return &ToolResult{OK: true, Data: map[string]any{"deleted": a.ID}}
}

// deleteSubtree removes a node and all of its descendants, cleaning up
// children lists and the scope.
func deleteSubtree(t *flow.Tree, node *flow.Node, scope map[string]bool) {
	var collect []*flow.Node
	var walk func(n *flow.Node)
	walk = func(n *flow.Node) {
		collect = append(collect, n)
		for _, c := range n.Children {
			if cn, ok := t.Nodes[c]; ok {
				walk(cn)
			}
		}
	}
	walk(node)
	parent := node.Parent
	for _, n := range collect {
		if p, ok := t.Nodes[n.Parent]; ok {
			p.Children = removeString(p.Children, n.ID)
		}
		delete(t.Nodes, n.ID)
		delete(scope, n.ID)
	}
	if parent != "" {
		if _, ok := t.Nodes[parent]; !ok {
			// parent subtree also gone (deleteSubtree callers pass root of scope)
		}
	}
}

func removeString(xs []string, s string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func execLinkNodes(ctx *ToolContext, raw json.RawMessage) *ToolResult {
	var a struct {
		Parent string `json:"parent"`
		Child  string `json:"child"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Parent == "" || a.Child == "" {
		return toolError(`{"parent":"nA","child":"nB"}`, "参数格式错误: 需要 parent 与 child 节点 id")
	}
	pn, ok := ctx.Tree.Nodes[a.Parent]
	if !ok {
		return toolError("parent 必须是已存在节点 id", "父节点不存在: %s", a.Parent)
	}
	cn, ok := ctx.Tree.Nodes[a.Child]
	if !ok {
		return toolError("child 必须是已存在节点 id", "子节点不存在: %s", a.Child)
	}
	if a.Parent == a.Child {
		return toolError("", "节点不能链接到自身")
	}
	if a.Child == ctx.Tree.Start {
		return toolError("", "start 节点不可作为子节点")
	}
	if !inScope(ctx, a.Parent) || !inScope(ctx, a.Child) {
		var outOfScope []string
		if !inScope(ctx, a.Parent) {
			outOfScope = append(outOfScope, a.Parent)
		}
		if !inScope(ctx, a.Child) {
			outOfScope = append(outOfScope, a.Child)
		}
		toolArgs, _ := json.Marshal(a)
		return &ToolResult{
			Paused: true,
			Questions: []PauseQuestion{{
				ID:        "scope-link-" + strings.Join(outOfScope, "-"),
				Type:      "scope",
				Question:  fmt.Sprintf("Agent 想连线 %s → %s，但节点 %s 不在你勾选的修改范围内。是否允许？", a.Parent, a.Child, strings.Join(outOfScope, ",")),
				Options:   []string{"允许", "拒绝", "允许本次全部越界节点"},
				NodeID:    strings.Join(outOfScope, ","),
				NodeType:  "link",
				Operation: toolLinkNodes,
				ToolArgs:  string(toolArgs),
			}},
		}
	}
	if ctx.Tree.IsDescendant(a.Child, a.Parent) {
		return toolError("", "连线会造成环路: %s 已是 %s 的后代", a.Parent, a.Child)
	}
	if cn.Parent != "" && cn.Parent != a.Parent {
		// 允许重新设父节点(如 try/catch 包裹时需要):从旧父节点的 children 列表中移除,
		// 再挂到新父节点下。IsDescendant 检查已防止环路。
		if oldParent, ok := ctx.Tree.Nodes[cn.Parent]; ok {
			oldParent.Children = removeString(oldParent.Children, a.Child)
		}
	}
	ctx.Tree.AddChild(a.Parent, a.Child)
	// 从 parent 指针全量重建 children 列表,消除旧父节点残留的连线,
	// 避免前端在下一次 get_flow 时看到短暂环路。
	ctx.Tree.ReconcileChildren()
	return &ToolResult{OK: true, Data: map[string]any{
		"parent_io": nodeIO(pn),
		"child_io":  nodeIO(cn),
		"linked":    []string{a.Parent, a.Child},
	}}
}

// nodeIO renders a node's input/output contracts for the LLM.
func nodeIO(n *flow.Node) map[string]any {
	return map[string]any{
		"id":      n.ID,
		"type":    n.Type,
		"inputs":  n.Inputs,
		"outputs": n.Outputs,
	}
}

// validateAPIBodyShape rejects the ambiguous whole-body wrapper when the unit
// exposes named body properties. A real property named body remains valid.
func validateAPIBodyShape(db *gorm.DB, n *flow.Node) error {
	if n.Type != flow.NodeAPI || db == nil {
		return nil
	}
	var ref struct {
		UnitID uint `json:"unit_id"`
	}
	if err := flow.UnmarshalConfig(n, &ref); err != nil || ref.UnitID == 0 {
		return nil
	}
	var unit model.TestUnit
	if err := db.Unscoped().First(&unit, ref.UnitID).Error; err != nil {
		return nil
	}
	derived := deriveInputs(unit)
	if _, hasBodyProperty := derived["body"]; !hasBodyProperty {
		if _, ok := n.Inputs["body"]; ok {
			return fmt.Errorf("API 单元 %d 的 body 是请求体容器，不能作为请求体字段；请改为使用 request_body.properties 中的真实字段", ref.UnitID)
		}
		var cfg struct {
			Params map[string]any `json:"params"`
		}
		if err := flow.UnmarshalConfig(n, &cfg); err == nil {
			if _, ok := cfg.Params["body"]; ok {
				return fmt.Errorf("API 单元 %d 的 config.params.body 会造成 body 套 body，请改用真实 body 字段", ref.UnitID)
			}
		}
	}
	return nil
}

func execValidateFlow(ctx *ToolContext) *ToolResult {
	log.Infof("agent: 主动 checkpoint/validate_flow 校验开始")
	res := flow.Validate(ctx.Tree, flow.ValidatorOptions{
		UnitDeleted: func(unitID uint) bool {
			var unit model.TestUnit
			return ctx.DB.Unscoped().First(&unit, unitID).Error == nil && unit.DeletedAt.Valid
		},
	})
	// flow 包本身不依赖数据库，API body schema 校验在 service 层补充。
	for id, n := range ctx.Tree.Nodes {
		if err := validateAPIBodyShape(ctx.DB, n); err != nil {
			res.Errors = append(res.Errors, flow.ValidationError{
				NodeID: id, Code: "api.body_wrapper", Level: flow.LevelError,
				Message: err.Error(), ExpectedFormat: `API body 应使用 schema.properties 的真实字段，不能重复包装 body`,
			})
		}
	}
	if res.HasErrors() {
		for _, e := range res.Errors {
			log.Errorf("agent: validate_flow 校验失败 node=%s code=%s level=%s msg=%s expected=%s", e.NodeID, e.Code, e.Level, e.Message, e.ExpectedFormat)
		}
	}
	for _, w := range res.Warnings {
		log.Warnf("agent: validate_flow 警告 node=%s code=%s msg=%s", w.NodeID, w.Code, w.Message)
	}
	log.Infof("agent: 主动 checkpoint/validate_flow 校验完成(valid=%v errors=%d warnings=%d)", !res.HasErrors(), len(res.Errors), len(res.Warnings))
	out := map[string]any{
		"valid":    !res.HasErrors(),
		"errors":   res.Errors,
		"warnings": res.Warnings,
	}
	if res.HasErrors() {
		return &ToolResult{OK: true, Data: out}
	}
	return &ToolResult{OK: true, Data: out}
}

// inScope reports whether a node is editable under the current selected scope.
// A nil/empty scope means unrestricted editing.
func inScope(ctx *ToolContext, id string) bool {
	if len(ctx.Scope) == 0 {
		return true
	}
	return ctx.Scope[id]
}
