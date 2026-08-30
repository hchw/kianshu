package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github/hchw/kianshu/internal/service"
)

// handleGlobalRuns lists execution records across accessible test sets.
func (s *Server) handleGlobalRuns(c *gin.Context) {
	uid, _ := userIDOf(c)
	var f service.GlobalRunFilter
	if value, err := strconv.ParseUint(c.Query("test_set_id"), 10, 32); err == nil {
		f.TestSetID = uint(value)
	}
	if value, err := strconv.ParseUint(c.Query("flow_id"), 10, 32); err == nil {
		f.FlowID = uint(value)
	}
	f.Status = c.Query("status")
	f.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	f.PageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if value := c.Query("from"); value != "" {
		t, err := parseRunDate(value, false)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "无效的开始时间")
			return
		}
		f.From = &t
	}
	if value := c.Query("to"); value != "" {
		t, err := parseRunDate(value, true)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "无效的结束时间")
			return
		}
		f.To = &t
	}
	rows, total, err := service.ListAccessibleRuns(s.DB, uid, f)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询执行记录失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"runs": rows, "total": total, "page": f.Page, "page_size": f.PageSize})
}

func (s *Server) handleGlobalRun(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "runID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的执行日志 ID")
		return
	}
	run, err := service.GetAccessibleRun(s.DB, uid, id)
	if err != nil {
		writeErr(c, http.StatusNotFound, "执行日志不存在")
		return
	}
	writeJSON(c, http.StatusOK, run)
}

func parseRunDate(value string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}
