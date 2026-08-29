package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github/hchw/kianshu/internal/service"
)

// handleDashboard returns the authenticated user's workspace summary.
//
//	@Summary	获取工作台
//	@Description	返回当前用户有权限访问的测试资产汇总、引导、最近工作和关注事项。
//	@Tags		工作台
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	service.DashboardData
//	@Failure	500	{object}	errorResp
//	@Router		/dashboard [get]
func (s *Server) handleDashboard(c *gin.Context) {
	uid, _ := userIDOf(c)
	data, err := service.Dashboard(s.DB, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "获取工作台失败")
		return
	}
	writeJSON(c, http.StatusOK, data)
}
