package httpapi

import (
	"errors"
	"net/http"

	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

// handleListSchedules returns the schedules of a flow.
// handleListSchedules returns the schedules of a flow.
//
//	@Summary	列出定时调度
//	@Description	返回流的全部 cron 调度任务。需要读权限。
//	@Tags		定时调度
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	scheduleListResp	"调度任务列表"
//	@Failure	400	{object}	errorResp			"无效的流 ID"
//	@Failure	403	{object}	errorResp			"无权访问该流"
//	@Failure	500	{object}	errorResp			"查询失败"
//	@Router		/flow/flows/{flowID}/schedules [get]
func (s *Server) handleListSchedules(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	schedules, err := s.Schedules.ListSchedules(flowID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"schedules": schedules})
}

// handleCreateSchedule adds a cron schedule to a flow.
// handleCreateSchedule adds a cron schedule to a flow.
//
//	@Summary	创建定时调度
//	@Description	为流注册一个 cron 表达式调度,创建后即启用。需要编辑权限。
//	@Tags		定时调度
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint				true	"流 ID"
//	@Param		body	body	createScheduleReq	true	"cron 表达式"
//	@Success	201	{object}	model.FlowSchedule	"创建的调度任务"
//	@Failure	400	{object}	errorResp			"cron 表达式不能为空 / 无效"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	500	{object}	errorResp			"创建失败"
//	@Router		/flow/flows/{flowID}/schedules [post]
func (s *Server) handleCreateSchedule(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req struct {
		Cron string `json:"cron"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Cron == "" {
		writeErr(c, http.StatusBadRequest, "cron 表达式不能为空")
		return
	}
	schedule, err := s.Schedules.CreateSchedule(flowID, req.Cron)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCron) {
			writeErr(c, http.StatusBadRequest, "cron 表达式无效")
			return
		}
		writeErr(c, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(c, http.StatusCreated, schedule)
}

// handleUpdateSchedule changes a schedule's cron expression.
// handleUpdateSchedule changes a schedule's cron expression.
//
//	@Summary	更新定时调度
//	@Description	修改指定调度任务的 cron 表达式。需要编辑权限。
//	@Tags		定时调度
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint				true	"流 ID"
//	@Param		scheduleID	path	uint				true	"调度任务 ID"
//	@Param		body		body	createScheduleReq	true	"新的 cron 表达式"
//	@Success	200	{object}	model.FlowSchedule	"更新后的调度任务"
//	@Failure	400	{object}	errorResp			"无效的 ID / cron 表达式无效"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	404	{object}	errorResp			"调度任务不存在"
//	@Failure	500	{object}	errorResp			"更新失败"
//	@Router		/flow/flows/{flowID}/schedules/{scheduleID} [patch]
func (s *Server) handleUpdateSchedule(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	scheduleID, ok := parseID(c, "scheduleID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的调度任务 ID")
		return
	}
	var req struct {
		Cron string `json:"cron"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Cron == "" {
		writeErr(c, http.StatusBadRequest, "cron 表达式不能为空")
		return
	}
	schedule, err := s.Schedules.UpdateSchedule(flowID, scheduleID, req.Cron)
	if err != nil {
		if errors.Is(err, service.ErrScheduleNotFound) {
			writeErr(c, http.StatusNotFound, "调度任务不存在")
			return
		}
		if errors.Is(err, service.ErrInvalidCron) {
			writeErr(c, http.StatusBadRequest, "cron 表达式无效")
			return
		}
		writeErr(c, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(c, http.StatusOK, schedule)
}

// handleSetScheduleEnabled pauses (false) or resumes (true) a schedule.
// handleSetScheduleEnabled pauses (false) or resumes (true) a schedule.
//
//	@Summary	启停定时调度
//	@Description	设置调度任务是否触发(暂停/恢复)。需要编辑权限。
//	@Tags		定时调度
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint					true	"流 ID"
//	@Param		scheduleID	path	uint					true	"调度任务 ID"
//	@Param		body		body	setScheduleEnabledReq	true	"是否启用"
//	@Success	200	{object}	model.FlowSchedule	"更新后的调度任务"
//	@Failure	400	{object}	errorResp			"无效的 ID / 请求体不合法"
//	@Failure	403	{object}	errorResp			"无编辑权限"
//	@Failure	404	{object}	errorResp			"调度任务不存在"
//	@Failure	500	{object}	errorResp			"更新失败"
//	@Router		/flow/flows/{flowID}/schedules/{scheduleID}/enabled [patch]
func (s *Server) handleSetScheduleEnabled(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	scheduleID, ok := parseID(c, "scheduleID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的调度任务 ID")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "无效的请求")
		return
	}
	schedule, err := s.Schedules.SetScheduleEnabled(flowID, scheduleID, req.Enabled)
	if err != nil {
		if errors.Is(err, service.ErrScheduleNotFound) {
			writeErr(c, http.StatusNotFound, "调度任务不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(c, http.StatusOK, schedule)
}

// handleDeleteSchedule removes a schedule.
// handleDeleteSchedule removes a schedule.
//
//	@Summary	删除定时调度
//	@Description	删除指定调度任务,不再触发。需要编辑权限。
//	@Tags		定时调度
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID		path	uint	true	"流 ID"
//	@Param		scheduleID	path	uint	true	"调度任务 ID"
//	@Success	200	{object}	deletedResp	"已删除"
//	@Failure	400	{object}	errorResp	"无效的 ID"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	404	{object}	errorResp	"调度任务不存在"
//	@Failure	500	{object}	errorResp	"删除失败"
//	@Router		/flow/flows/{flowID}/schedules/{scheduleID} [delete]
func (s *Server) handleDeleteSchedule(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	scheduleID, ok := parseID(c, "scheduleID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的调度任务 ID")
		return
	}
	if err := s.Schedules.DeleteSchedule(flowID, scheduleID); err != nil {
		if errors.Is(err, service.ErrScheduleNotFound) {
			writeErr(c, http.StatusNotFound, "调度任务不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"deleted": true})
}
