package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github/hchw/kianshu/internal/exec"
	"github/hchw/kianshu/internal/flow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

// ErrRunNotFound indicates a missing execution log.
var ErrRunNotFound = errors.New("执行日志不存在")

// HTTPCallAPI returns the real outbound CallAPI implementation. When a test
// host is empty, api nodes fail with an explanatory error. A positive timeout
// bounds each HTTP call so a hung upstream cannot stall a run forever.
// 无论 HTTP 状态码如何,只要网络请求成功(无连接/超时错误)都返回响应;
// 错误码/响应体的判断由下游断言节点负责。
func HTTPCallAPI(host string, timeout time.Duration) func(exec.APICall) (*exec.APIResponse, error) {
	client := &http.Client{}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return func(call exec.APICall) (*exec.APIResponse, error) {
		u, err := url.Parse(call.URL)
		if err != nil {
			return nil, err
		}
		if u.Host == "" {
			return nil, fmt.Errorf("目标 host 为空,无法发起请求")
		}
		q := u.Query()
		for k, v := range call.Query {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		u.RawQuery = q.Encode()

		var body io.Reader
		if call.Body != nil {
			b, err := json.Marshal(call.Body)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequest(call.Method, u.String(), body)
		if err != nil {
			return nil, err
		}
		for k, v := range call.Headers {
			req.Header.Set(k, v)
		}
		if call.Body != nil {
			req.Header.Set("Content-Type", "application/json")
			b, _ := json.Marshal(call.Body)
			log.Printf("[req] body %s", string(b))
		}
		for k, v := range call.Headers {
			log.Printf("[req] header %s: %s", k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		log.Printf("[resp] %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
		var respBody any
		if err := json.Unmarshal(data, &respBody); err != nil {
			respBody = string(data)
		}
		return &exec.APIResponse{StatusCode: resp.StatusCode, Body: respBody}, nil
	}
}

// TrialRun executes the current draft of a flow, snapshots its api nodes in
// memory (no version is produced), and records an execution log bound to
// version_id = 0. host is the test set's execution host.
func TrialRun(db *gorm.DB, flowID uint, execTimeout time.Duration) (*model.ExecutionLog, error) {
	d, err := GetDraft(db, flowID)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(d.Tree)
	if err != nil {
		return nil, err
	}
	if err := SnapshotAPINodes(db, tree); err != nil {
		return nil, err
	}
	var ts model.TestSet
	if err := db.First(&ts, flowTestSetID(db, flowID)).Error; err != nil {
		return nil, err
	}
	return runAndLog(db, flowID, 0, 0, tree, ts.Host, execTimeout)
}

// RunVersion executes the version snapshot identified by versionNo and
// records an execution log bound to its version_id.
func RunVersion(db *gorm.DB, flowID uint, versionNo int, execTimeout time.Duration) (*model.ExecutionLog, error) {
	v, err := GetVersion(db, flowID, versionNo)
	if err != nil {
		return nil, err
	}
	tree, err := flow.ParseTree(v.Tree)
	if err != nil {
		return nil, err
	}
	var ts model.TestSet
	if err := db.First(&ts, flowTestSetID(db, flowID)).Error; err != nil {
		return nil, err
	}
	return runAndLog(db, flowID, v.ID, versionNo, tree, ts.Host, execTimeout)
}

// flowTestSetID resolves the test set a flow belongs to.
func flowTestSetID(db *gorm.DB, flowID uint) uint {
	var f struct {
		TestSetID uint
	}
	if err := db.Table("test_flows").Select("test_set_id").Where("id = ?", flowID).Scan(&f).Error; err != nil {
		return 0
	}
	return f.TestSetID
}

// runAndLog executes a tree and persists the outcome as an ExecutionLog. The
// whole run is bounded by execTimeout (non-zero) via a context, so non-network
// nodes (loop, adapter) cannot hang indefinitely either.
func runAndLog(db *gorm.DB, flowID, versionID uint, versionNo int, tree *flow.Tree, host string, execTimeout time.Duration) (*model.ExecutionLog, error) {
	started := time.Now()
	ctx := context.Background()
	if execTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, execTimeout)
		defer cancel()
	}
	res, err := exec.Run(ctx, tree, exec.Options{Host: host, CallAPI: HTTPCallAPI(host, execTimeout)})
	finished := time.Now()
	if err != nil {
		return nil, err
	}
	nodeResults, err := json.Marshal(res.Results)
	if err != nil {
		return nil, err
	}
	log := &model.ExecutionLog{
		FlowID:      flowID,
		VersionID:   versionID,
		VersionNo:   versionNo,
		Status:      string(res.Status),
		Tree:        tree.String(),
		NodeResults: string(nodeResults),
		StartedAt:   started,
		FinishedAt:  finished,
	}
	if err := db.Create(log).Error; err != nil {
		return nil, err
	}
	return log, nil
}

// ListRuns returns the execution logs of a flow, newest first, with
// optional pagination. page is 1-indexed; pageSize defaults to 20, max 100.
// total is the total count of matching records.
func ListRuns(db *gorm.DB, flowID uint, page, pageSize int) ([]model.ExecutionLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int64
	if err := db.Model(&model.ExecutionLog{}).Where("flow_id = ?", flowID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []model.ExecutionLog
	offset := (page - 1) * pageSize
	if err := db.Where("flow_id = ?", flowID).Order("id desc").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// GetRun returns one execution log by id.
type GlobalRunFilter struct {
	TestSetID uint
	FlowID    uint
	Status    string
	From      *time.Time
	To        *time.Time
	Page      int
	PageSize  int
}

type GlobalRun struct {
	model.ExecutionLog
	TestSetID   uint   `json:"test_set_id"`
	TestSetName string `json:"test_set_name"`
	FlowName    string `json:"flow_name"`
	Role        string `json:"role"`
}

// ListAccessibleRuns returns execution logs in the user's accessible test sets.
func ListAccessibleRuns(db *gorm.DB, userID uint, filter GlobalRunFilter) ([]GlobalRun, int64, error) {
	sets, err := accessibleDashboardSets(db, userID)
	if err != nil {
		return nil, 0, err
	}
	setIDs := dashboardSetIDs(sets)
	if filter.TestSetID != 0 {
		found := false
		for _, id := range setIDs {
			if id == filter.TestSetID {
				found = true
				break
			}
		}
		if !found {
			return []GlobalRun{}, 0, nil
		}
		setIDs = []uint{filter.TestSetID}
	}
	q := db.Model(&model.ExecutionLog{}).Joins("JOIN test_flows ON test_flows.id = execution_logs.flow_id").Where("test_flows.test_set_id IN (?)", setIDs)
	if filter.FlowID != 0 {
		q = q.Where("execution_logs.flow_id = ?", filter.FlowID)
	}
	if filter.Status != "" {
		status := filter.Status
		if status == "success" {
			status = "ok"
		}
		q = q.Where("execution_logs.status = ?", status)
	}
	if filter.From != nil {
		q = q.Where("execution_logs.started_at >= ?", *filter.From)
	}
	if filter.To != nil {
		q = q.Where("execution_logs.started_at < ?", *filter.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	var rows []GlobalRun
	if err := q.Select("execution_logs.*, test_flows.test_set_id, test_flows.name AS flow_name").Order("execution_logs.started_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	for i := range rows {
		var ts model.TestSet
		if err := db.First(&ts, rows[i].TestSetID).Error; err == nil {
			rows[i].TestSetName = ts.Name
		}
		for _, s := range sets {
			if s.model.ID == rows[i].TestSetID {
				rows[i].Role = roleName(s.role)
				break
			}
		}
	}
	return rows, total, nil
}

func GetAccessibleRun(db *gorm.DB, userID, runID uint) (*GlobalRun, error) {
	sets, err := accessibleDashboardSets(db, userID)
	if err != nil {
		return nil, err
	}
	ids := dashboardSetIDs(sets)
	var row GlobalRun
	query := db.Model(&model.ExecutionLog{}).Joins("JOIN test_flows ON test_flows.id = execution_logs.flow_id").Where("execution_logs.id = ? AND test_flows.test_set_id IN (?)", runID, ids).Select("execution_logs.*, test_flows.test_set_id, test_flows.name AS flow_name")
	if err := query.First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	var ts model.TestSet
	if err := db.First(&ts, row.TestSetID).Error; err == nil {
		row.TestSetName = ts.Name
	}
	for _, s := range sets {
		if s.model.ID == row.TestSetID {
			row.Role = roleName(s.role)
			break
		}
	}
	return &row, nil
}

func GetRun(db *gorm.DB, runID uint) (*model.ExecutionLog, error) {
	var log model.ExecutionLog
	if err := db.First(&log, runID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return &log, nil
}
