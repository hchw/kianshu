package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

// cronDescribeResp describes a cron expression with next execution times.
type cronDescribeResp struct {
	Valid       bool     `json:"valid"`
	Description string   `json:"description"`
	NextRuns    []string `json:"next_runs"`
}

// handleDescribeCron validates a cron expression and returns a human-readable
// description together with the next 5 execution times.
//
//	@Summary	解析 cron 表达式
//	@Description	验证 cron 表达式并返回描述和未来 5 次执行时间。
//	@Tags		定时调度
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body	createScheduleReq	true	"cron 表达式"
//	@Success	200	{object}	cronDescribeResp	"描述信息"
//	@Failure	400	{object}	errorResp		"请求体无效"
//	@Router		/utils/cron/describe [post]
func (s *Server) handleDescribeCron(c *gin.Context) {
	var req struct {
		Cron string `json:"cron"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Cron == "" {
		writeErr(c, http.StatusBadRequest, "cron 表达式不能为空")
		return
	}
	resp := describeCron(req.Cron)
	writeJSON(c, http.StatusOK, resp)
}

const cronSamples = 5

func describeCron(expr string) cronDescribeResp {
	parser := cronParser(expr)
	sched, err := parser.Parse(expr)
	if err != nil {
		return cronDescribeResp{
			Valid:       false,
			Description: "无效的 cron 表达式",
			NextRuns:    nil,
		}
	}
	now := time.Now()
	next := make([]string, 0, cronSamples)
	t := now
	for i := 0; i < cronSamples; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		next = append(next, t.Local().Format("2006-01-02 15:04:05"))
	}
	return cronDescribeResp{
		Valid:       true,
		Description: describeCronHuman(expr),
		NextRuns:    next,
	}
}

func cronParser(expr string) cron.Parser {
	if len(strings.Fields(expr)) == 6 {
		return cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	}
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

// describeCronHuman returns a simple Chinese description of a 5- or 6-field cron expression.
func describeCronHuman(expr string) string {
	parser := cronParser(expr)
	_, err := parser.Parse(expr)
	if err != nil {
		return "无效的 cron 表达式"
	}
	// 5-field cron: minute hour day month weekday
	fields := cronFieldNames(expr)
	if fields == nil {
		return expr
	}
	hasSec := len(fields) == 6
	var sec, m, h, d, mo, w string
	if hasSec {
		sec, m, h, d, mo, w = fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]
	} else {
		m, h, d, mo, w = fields[0], fields[1], fields[2], fields[3], fields[4]
	}

	// Build a readable Chinese description
	parts := []string{}

	// Month
	if mo == "*" {
		// every month - no prefix
	} else {
		parts = append(parts, mo+"月")
	}

	// Day of month vs weekday
	hasDOM := d != "*"
	hasDOW := w != "*"

	if hasDOM && hasDOW {
		parts = append(parts, d+"号或周"+dowCN(w))
	} else if hasDOM {
		parts = append(parts, "每月"+d+"号")
	} else if hasDOW {
		parts = append(parts, "每周"+dowCN(w))
	} else {
		parts = append(parts, "每天")
	}

	// Time
	t := formatTime(h, m, sec, hasSec)
	parts = append(parts, "的"+t)

	if len(parts) == 1 && parts[0] == "每天" {
		parts = append(parts, "的"+formatTime(h, m, sec, hasSec))
	}

	return joinParts(parts)
}

func cronFieldNames(expr string) []string {
	parts := splitCron(expr)
	if len(parts) < 5 || len(parts) > 6 {
		return nil
	}
	return parts
}

func splitCron(expr string) []string {
	return strings.Fields(expr)
}

func dowCN(w string) string {
	switch w {
	case "0", "7":
		return "日"
	case "1":
		return "一"
	case "2":
		return "二"
	case "3":
		return "三"
	case "4":
		return "四"
	case "5":
		return "五"
	case "6":
		return "六"
	default:
		return w
	}
}

func formatTime(h, m, sec string, hasSec bool) string {
	// 有秒字段时，每 N 秒
	if hasSec && sec != "*" && sec != "0" && m == "*" && h == "*" {
		return "每" + sec + "秒"
	}
	if hasSec && h == "0" && m == "0" && sec == "0" {
		return "零点整"
	}
	if hasSec && sec != "*" && m != "*" && h != "*" {
		return h + ":" + pad2(m) + ":" + pad2(sec)
	}
	if m == "0" && h == "0" {
		return "零点整"
	}
	if m == "0" {
		return h + ":00"
	}
	if m == "*" && h == "*" {
		return "每分钟"
	}
	if m == "*" {
		return h + "时每分钟"
	}
	if h == "*" {
		return "每小时的第" + m + "分"
	}
	return h + ":" + pad2(m)
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func joinParts(parts []string) string {
	result := ""
	for _, p := range parts {
		result += p
	}
	return result
}
