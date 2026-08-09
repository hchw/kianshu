package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

// handleListFlows lists the flows inside a test set.
//
//	@Summary	列出测试流
//	@Description	返回测试集内的所有测试流(仅元信息,执行树在草稿/版本中)。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	uint	true	"测试集 ID"
//	@Success	200	{object}	flowListResp	"测试流列表"
//	@Failure	400	{object}	errorResp		"无效的测试集 ID"
//	@Failure	403	{object}	errorResp		"无权访问该测试集"
//	@Failure	500	{object}	errorResp		"查询失败"
//	@Router		/test-sets/{id}/flows [get]
func (s *Server) handleListFlows(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	var flows []model.TestFlow
	if err := s.DB.Where("test_set_id = ?", setID).Order("id").Find(&flows).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"flows": flows})
}

// handleCreateFlow creates a flow inside a test set.
//
//	@Summary	创建测试流
//	@Description	在测试集内创建一个测试流。需要编辑权限。
//	@Tags		测试流
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint			true	"测试集 ID"
//	@Param		body	body	createFlowReq	true	"流名称"
//	@Success	201	{object}	model.TestFlow	"创建的测试流"
//	@Failure	400	{object}	errorResp		"名称不能为空 / 无效的测试集 ID"
//	@Failure	403	{object}	errorResp		"无编辑权限"
//	@Failure	500	{object}	errorResp		"创建失败"
//	@Router		/test-sets/{id}/flows [post]
func (s *Server) handleCreateFlow(c *gin.Context) {
	uid, _ := userIDOf(c)
	testSetID, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	canEdit, err := service.CanEdit(s.DB, testSetID, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		writeErr(c, http.StatusBadRequest, "名称不能为空")
		return
	}
	f, err := service.CreateFlow(s.DB, testSetID, uid, req.Name)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(c, http.StatusCreated, f)
}

// flowOf reads the flowID param and verifies read access to its test set.
func (s *Server) flowReadable(c *gin.Context) (uint, uint, bool) {
	uid, _ := userIDOf(c)
	flowID, ok := parseID(c, "flowID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的流 ID")
		return 0, 0, false
	}
	ts, ok := s.flowTestSet(flowID)
	if !ok {
		writeErr(c, http.StatusNotFound, "流不存在")
		return 0, 0, false
	}
	canRead, err := service.CanRead(s.DB, ts, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, 0, false
	}
	if !canRead {
		writeErr(c, http.StatusForbidden, "无权访问该流")
		return 0, 0, false
	}
	return flowID, uid, true
}

// flowEditable verifies the user may edit the flow's test set.
func (s *Server) flowEditable(c *gin.Context) (uint, uint, bool) {
	flowID, uid, ok := s.flowReadable(c)
	if !ok {
		return 0, 0, false
	}
	ts, _ := s.flowTestSet(flowID)
	canEdit, err := service.CanEdit(s.DB, ts, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, 0, false
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return 0, 0, false
	}
	return flowID, uid, true
}

func (s *Server) flowTestSet(flowID uint) (uint, bool) {
	var f struct {
		TestSetID uint
	}
	if err := s.DB.Table("test_flows").Select("test_set_id").Where("id = ?", flowID).Scan(&f).Error; err != nil {
		return 0, false
	}
	return f.TestSetID, f.TestSetID != 0
}

// handleDeleteFlow hard-deletes a flow together with its draft, versions,
// execution logs and schedules, and cancels registered cron jobs.
//
//	@Summary	删除测试流
//	@Description	硬删除指定测试流及其草稿、版本、运行记录与定时调度,并取消已注册的定时任务。需要编辑权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	deletedResp	"已删除"
//	@Failure	400	{object}	errorResp	"无效的流 ID"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	404	{object}	errorResp	"流不存在"
//	@Failure	500	{object}	errorResp	"删除失败"
//	@Router		/flow/flows/{flowID} [delete]
func (s *Server) handleDeleteFlow(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	if err := service.DeleteFlow(s.DB, s.Schedules, flowID); err != nil {
		if errors.Is(err, service.ErrFlowNotFound) {
			writeErr(c, http.StatusNotFound, "流不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"deleted": true})
}

// handleGetDraft returns a flow's working draft (editable tree snapshot).
//
//	@Summary	获取流草稿
//	@Description	返回流的可编辑草稿,tree 为整棵执行树快照(JSON 字符串)。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	draftView		"草稿内容"
//	@Failure	400	{object}	errorResp		"无效的流 ID"
//	@Failure	403	{object}	errorResp		"无权访问该流"
//	@Failure	404	{object}	errorResp		"草稿不存在"
//	@Router		/flow/flows/{flowID}/draft [get]
func (s *Server) handleGetDraft(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	ts, _ := s.flowTestSet(flowID)
	d, err := service.GetDraft(s.DB, flowID)
	if err != nil {
		writeErr(c, http.StatusNotFound, "草稿不存在")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"flow_id": flowID, "test_set_id": ts, "name": d.Name, "tree": d.Tree})
}

// handleUpdateDraft saves a flow's working draft. Each save creates a new
// draft revision; enabling a version is a separate action.
//
//	@Summary	更新流草稿
//	@Description	保存流的可编辑草稿。tree 可为内联 JSON 对象或 JSON 编码字符串。需要编辑权限。
//	@Tags		测试流
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint			true	"流 ID"
//	@Param		body	body	updateDraftReq	true	"草稿内容(必须包含 tree)"
//	@Success	200	{object}	model.FlowDraft	"保存后的草稿记录"
//	@Failure	400	{object}	errorResp		"请求体不合法 / tree 格式不合法 / 校验失败"
//	@Failure	403	{object}	errorResp		"无编辑权限"
//	@Failure	500	{object}	errorResp		"保存草稿失败"
//	@Router		/flow/flows/{flowID}/draft [put]
func (s *Server) handleUpdateDraft(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req struct {
		Name string          `json:"name"`
		Tree json.RawMessage `json:"tree"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Tree) == 0 {
		writeErr(c, http.StatusBadRequest, "请求体需包含 tree")
		return
	}
	treeJSON, err := treePayload(req.Tree)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "tree 格式不合法")
		return
	}
	d, err := service.UpdateDraft(s.DB, flowID, req.Name, treeJSON)
	if err != nil {
		if errors.Is(err, service.ErrFlowValidation) {
			writeErr(c, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(c, http.StatusInternalServerError, "保存草稿失败")
		return
	}
	writeJSON(c, http.StatusOK, d)
}

// treePayload accepts a tree either as an inline JSON object or as a JSON
// string, and returns the canonical JSON string form.
func treePayload(raw json.RawMessage) (string, error) {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", err
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return "", err
		}
		return v, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", err
	}
	return string(raw), nil
}

// handleValidateDraft runs whole-tree validation over the current draft.
//
//	@Summary	校验流草稿
//	@Description	对当前草稿执行全树校验(单根、无环、I/O 契约、JSONata 语法、try/catch 配对、loop 输入、缓存键静态可见、软删单元告警)。返回 errors 与 warnings 两类发现。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	flow.Result	"校验结果(errors / warnings)"
//	@Failure	400	{object}	errorResp	"无效的流 ID / 草稿解析失败"
//	@Failure	403	{object}	errorResp	"无权访问该流"
//	@Failure	404	{object}	errorResp	"草稿不存在"
//	@Router		/flow/flows/{flowID}/draft/validate [post]
func (s *Server) handleValidateDraft(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	d, err := service.GetDraft(s.DB, flowID)
	if err != nil {
		writeErr(c, http.StatusNotFound, "草稿不存在")
		return
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "草稿解析失败")
		return
	}
	// 清理旧版本恢复带入的无 source input（如 body），避免校验假阳性。
	service.StripOrphanInputs(tree)
	opts := flow.ValidatorOptions{
		UnitDeleted: func(unitID uint) bool {
			var u model.TestUnit
			if err := s.DB.Unscoped().First(&u, unitID).Error; err != nil {
				return false
			}
			return u.DeletedAt.Valid
		},
	}
	res := flow.Validate(tree, opts)
	writeJSON(c, http.StatusOK, res)
}

// handleSaveEnable turns the current draft into an immutable enabled version.
//
//	@Summary	保存并启用版本
//	@Description	将当前草稿固化为不可变版本并设为启用。校验失败时返回 422 及校验详情。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	201	{object}	model.FlowVersion	"固化的版本快照"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	422	{object}	errorResp			"流校验失败(含 validation 详情)"
//	@Failure	500	{object}	errorResp			"保存启用失败"
//	@Router		/flow/flows/{flowID}/versions [post]
func (s *Server) handleSaveEnable(c *gin.Context) {
	flowID, uid, ok := s.flowEditable(c)
	if !ok {
		return
	}
	version, res, err := service.SaveAndEnable(s.DB, flowID, uid)
	if err != nil {
		if errors.Is(err, service.ErrFlowValidation) {
			writeJSON(c, http.StatusUnprocessableEntity, gin.H{"error": "流校验失败", "validation": res})
			return
		}
		writeErr(c, http.StatusInternalServerError, "保存启用失败")
		return
	}
	writeJSON(c, http.StatusCreated, version)
}

// handleListVersions returns the immutable version snapshots of a flow.
//
//	@Summary	列出流版本
//	@Description	返回流的所有不可变版本快照。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	versionListResp	"版本列表"
//	@Failure	400	{object}	errorResp		"无效的流 ID"
//	@Failure	403	{object}	errorResp		"无权访问该流"
//	@Failure	500	{object}	errorResp		"查询版本失败"
//	@Router		/flow/flows/{flowID}/versions [get]
func (s *Server) handleListVersions(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	versions, err := service.ListVersions(s.DB, flowID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询版本失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"versions": versions})
}

// handleGetVersion returns one immutable version snapshot by its number.
//
//	@Summary	获取流版本
//	@Description	按版本号返回指定版本的自包含快照,可用于精确复原。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint	true	"流 ID"
//	@Param		versionNo	path	uint	true	"版本号"
//	@Success	200	{object}	model.FlowVersion	"版本快照"
//	@Failure	400	{object}	errorResp			"无效的 ID"
//	@Failure	403	{object}	errorResp			"无权访问该流"
//	@Failure	404	{object}	errorResp			"版本不存在"
//	@Failure	500	{object}	errorResp			"读取版本失败"
//	@Router		/flow/flows/{flowID}/versions/{versionNo} [get]
func (s *Server) handleGetVersion(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	versionNo, ok := parseID(c, "versionNo")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的版本号")
		return
	}
	v, err := service.GetVersion(s.DB, flowID, int(versionNo))
	if err != nil {
		if errors.Is(err, service.ErrFlowNotFound) {
			writeErr(c, http.StatusNotFound, "版本不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "读取版本失败")
		return
	}
	writeJSON(c, http.StatusOK, v)
}

// handleTrialRun runs the current draft without saving a version.
//
//	@Summary	草稿试运行
//	@Description	对当前草稿执行一次试运行,返回执行日志(含各节点结果)。不生成版本。需要编辑权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	201	{object}	model.ExecutionLog	"执行日志"
//	@Failure	400	{object}	errorResp			"无效的流 ID"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	500	{object}	errorResp			"试运行失败"
//	@Router		/flow/flows/{flowID}/draft/trial-run [post]
func (s *Server) handleTrialRun(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	log, err := service.TrialRun(s.DB, flowID, s.Cfg.ExecTimeout)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "试运行失败")
		return
	}
	s.RunBus.Publish(log)
	writeJSON(c, http.StatusCreated, log)
}

// handleRunVersion executes an immutable version snapshot.
//
//	@Summary	执行流版本
//	@Description	执行指定版本的快照并写入执行日志。需要编辑权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint	true	"流 ID"
//	@Param		versionNo	path	uint	true	"版本号"
//	@Success	201	{object}	model.ExecutionLog	"执行日志"
//	@Failure	400	{object}	errorResp			"无效的 ID"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	404	{object}	errorResp			"版本不存在"
//	@Failure	500	{object}	errorResp			"执行版本失败"
//	@Router		/flow/flows/{flowID}/versions/{versionNo}/run [post]
func (s *Server) handleRunVersion(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	versionNo, ok := parseID(c, "versionNo")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的版本号")
		return
	}
	log, err := service.RunVersion(s.DB, flowID, int(versionNo), s.Cfg.ExecTimeout)
	if err != nil {
		if errors.Is(err, service.ErrFlowNotFound) {
			writeErr(c, http.StatusNotFound, "版本不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "执行版本失败")
		return
	}
	s.RunBus.Publish(log)
	writeJSON(c, http.StatusCreated, log)
}

// handleListRuns returns the execution logs of a flow.
//
//	@Summary	列出执行日志
//	@Description	返回流的执行日志记录,支持分页。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint	true	"流 ID"
//	@Param		page		query	int	false	"页码(1开始,默认1)"
//	@Param		page_size	query	int	false	"每页条数(默认20,最大100)"
//	@Success	200	{object}	runListResp	"执行日志列表"
//	@Failure	400	{object}	errorResp	"无效的流 ID"
//	@Failure	403	{object}	errorResp	"无权访问该流"
//	@Failure	500	{object}	errorResp	"查询执行日志失败"
//	@Router		/flow/flows/{flowID}/runs [get]
func (s *Server) handleListRuns(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	logs, total, err := service.ListRuns(s.DB, flowID, page, pageSize)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询执行日志失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"runs": logs, "total": total, "page": page, "page_size": pageSize})
}

// handleGetRun returns one execution log of a flow.
//
//	@Summary	获取执行日志
//	@Description	返回单条执行日志(含关联版本快照与各节点结果),可用于复原。需要读权限。
//	@Tags		测试流
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Param		runID	path	uint	true	"执行日志 ID"
//	@Success	200	{object}	model.ExecutionLog	"执行日志"
//	@Failure	400	{object}	errorResp			"无效的 ID"
//	@Failure	403	{object}	errorResp			"无权访问该流"
//	@Failure	404	{object}	errorResp			"执行日志不存在"
//	@Failure	500	{object}	errorResp			"读取执行日志失败"
//	@Router		/flow/flows/{flowID}/runs/{runID} [get]
func (s *Server) handleGetRun(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	runID, ok := parseID(c, "runID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的执行日志 ID")
		return
	}
	log, err := service.GetRun(s.DB, runID)
	if err != nil {
		if errors.Is(err, service.ErrRunNotFound) {
			writeErr(c, http.StatusNotFound, "执行日志不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "读取执行日志失败")
		return
	}
	if log.FlowID != flowID {
		writeErr(c, http.StatusNotFound, "执行日志不存在")
		return
	}
	writeJSON(c, http.StatusOK, log)
}

// handleSubscribeRuns streams newly created execution logs as SSE so the
// frontend can update the run list in real time without polling.
//
//	@Summary	订阅执行日志（实时推送）
//	@Description	以 SSE 实时推送流的执行日志。新日志产生时立即推送到客户端。需要读权限。
//	@Tags		测试流
//	@Produce	text/event-stream
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{string}	string	"SSE 事件流"
//	@Failure	400	{object}	errorResp	"无效的流 ID"
//	@Failure	403	{object}	errorResp	"无权访问该流"
//	@Router		/flow/flows/{flowID}/runs/subscribe [get]
func (s *Server) handleSubscribeRuns(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()

	ctx := c.Request.Context()
	ch := s.RunBus.Subscribe(flowID)
	defer s.RunBus.Unsubscribe(flowID, ch)

	c.Stream(func(w io.Writer) bool {
		select {
		case logEntry := <-ch:
			data, _ := json.Marshal(logEntry)
			io.WriteString(w, "data: "+string(data)+"\n\n")
			return true
		case <-ctx.Done():
			return false
		}
	})
}
