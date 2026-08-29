package service

import (
	"sort"
	"time"

	"github/hchw/kianshu/internal/model"
	"gorm.io/gorm"
)

type DashboardSummary struct {
	TestSets         int `json:"test_sets"`
	Flows            int `json:"flows"`
	Units            int `json:"units"`
	RecentRuns       int `json:"recent_runs"`
	FailedRuns       int `json:"failed_runs"`
	EnabledSchedules int `json:"enabled_schedules"`
}

type DashboardStep struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Target      string `json:"target"`
}

type DashboardRecentWork struct {
	Kind        string    `json:"kind"`
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	TestSetID   uint      `json:"test_set_id"`
	TestSetName string    `json:"test_set_name"`
	UpdatedAt   time.Time `json:"updated_at"`
	Role        string    `json:"role"`
	Target      string    `json:"target"`
}

type DashboardAttention struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Target      string `json:"target"`
	CanEdit     bool   `json:"can_edit"`
}

type DashboardData struct {
	Summary        DashboardSummary      `json:"summary"`
	Onboarding     []DashboardStep       `json:"onboarding"`
	RecentWork     []DashboardRecentWork `json:"recent_work"`
	AttentionItems []DashboardAttention  `json:"attention_items"`
}

type dashboardSet struct {
	model model.TestSet
	role  RoleResult
}

func Dashboard(db *gorm.DB, userID uint) (*DashboardData, error) {
	sets, err := accessibleDashboardSets(db, userID)
	if err != nil {
		return nil, err
	}
	result := &DashboardData{Onboarding: []DashboardStep{
		{Key: "test-set", Title: "创建测试集", Description: "先建立一个接口测试工作区", Target: "/test-sets"},
		{Key: "swagger", Title: "导入 Swagger", Description: "从接口文档生成可测试的接口单元", Target: "/test-sets"},
		{Key: "provider", Title: "配置 Provider", Description: "连接 OpenAI-compatible Provider", Target: "/providers"},
		{Key: "flow", Title: "生成测试流", Description: "组合接口并验证一条业务链路", Target: "/test-sets"},
	}}
	result.Summary.TestSets = len(sets)
	var providers int64
	if err := db.Model(&model.Provider{}).Where("user_id = ? AND enabled = ?", userID, true).Count(&providers).Error; err != nil {
		return nil, err
	}
	var recentRuns, failedRuns int64
	week := time.Now().AddDate(0, 0, -7)
	flowScope := db.Model(&model.TestFlow{}).Select("id").Where("test_set_id IN ?", dashboardSetIDs(sets))
	if err := db.Model(&model.ExecutionLog{}).Where("flow_id IN (?) AND created_at >= ?", flowScope, week).Count(&recentRuns).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.ExecutionLog{}).Where("flow_id IN (?) AND created_at >= ? AND status = ?", flowScope, week, "failed").Count(&failedRuns).Error; err != nil {
		return nil, err
	}
	result.Summary.RecentRuns, result.Summary.FailedRuns = int(recentRuns), int(failedRuns)
	var recent []DashboardRecentWork
	for _, s := range sets {
		var flows []model.TestFlow
		if err := db.Where("test_set_id = ?", s.model.ID).Find(&flows).Error; err != nil {
			return nil, err
		}
		result.Summary.Flows += len(flows)
		var units int64
		if err := db.Model(&model.TestUnit{}).Where("test_set_id = ?", s.model.ID).Count(&units).Error; err != nil {
			return nil, err
		}
		result.Summary.Units += int(units)
		var schedules int64
		if err := db.Model(&model.FlowSchedule{}).Where("test_set_id = ? AND enabled = ?", s.model.ID, true).Count(&schedules).Error; err != nil {
			return nil, err
		}
		result.Summary.EnabledSchedules += int(schedules)
		if s.model.UpdatedAt.After(time.Time{}) {
			recent = append(recent, DashboardRecentWork{Kind: "test_set", ID: s.model.ID, Name: s.model.Name, TestSetID: s.model.ID, TestSetName: s.model.Name, UpdatedAt: s.model.UpdatedAt, Role: roleName(s.role), Target: "/test-sets/" + uintString(s.model.ID)})
		}
		for _, f := range flows {
			recent = append(recent, DashboardRecentWork{Kind: "flow", ID: f.ID, Name: f.Name, TestSetID: s.model.ID, TestSetName: s.model.Name, UpdatedAt: f.UpdatedAt, Role: roleName(s.role), Target: "/flows/" + uintString(f.ID)})
			var latest model.ExecutionLog
			if err := db.Where("flow_id = ?", f.ID).Order("created_at DESC").First(&latest).Error; err == nil && latest.Status == "failed" {
				result.AttentionItems = append(result.AttentionItems, DashboardAttention{Kind: "failed_run", Title: f.Name + " 执行失败", Description: "最近一次执行未通过", Target: "/flows/" + uintString(f.ID), CanEdit: s.role == RoleOwner || s.role == RoleEdit})
			}
		}
		if s.model.Host == "" {
			result.AttentionItems = append(result.AttentionItems, DashboardAttention{Kind: "host", Title: s.model.Name + " 未配置 Host", Description: "配置 Host 后才能执行接口请求", Target: "/test-sets/" + uintString(s.model.ID), CanEdit: s.role == RoleOwner || s.role == RoleEdit})
		}
	}
	result.SummaryOnboarding(db, userID, sets, providers)
	sort.Slice(recent, func(i, j int) bool { return recent[i].UpdatedAt.After(recent[j].UpdatedAt) })
	if len(recent) > 8 {
		recent = recent[:8]
	}
	result.RecentWork = recent
	if providers == 0 {
		result.AttentionItems = append(result.AttentionItems, DashboardAttention{Kind: "provider", Title: "尚未配置 Provider", Description: "配置 Provider 后可以使用 AI 生成测试流", Target: "/providers", CanEdit: true})
	}
	if len(result.AttentionItems) > 8 {
		result.AttentionItems = result.AttentionItems[:8]
	}
	return result, nil
}

func (d *DashboardData) SummaryOnboarding(db *gorm.DB, userID uint, sets []dashboardSet, providers int64) {
	if len(sets) > 0 {
		d.Onboarding[0].Done = true
	}
	var imports int64
	db.Model(&model.Import{}).Where("test_set_id IN ?", dashboardSetIDs(sets)).Count(&imports)
	d.Onboarding[1].Done = imports > 0
	d.Onboarding[2].Done = providers > 0
	var flows int64
	db.Model(&model.TestFlow{}).Where("test_set_id IN ?", dashboardSetIDs(sets)).Count(&flows)
	d.Onboarding[3].Done = flows > 0
}
func dashboardSetIDs(sets []dashboardSet) []uint {
	ids := make([]uint, 0, len(sets))
	for _, s := range sets {
		ids = append(ids, s.model.ID)
	}
	return ids
}
func accessibleDashboardSets(db *gorm.DB, userID uint) ([]dashboardSet, error) {
	var owned []model.TestSet
	if err := db.Where("owner_id = ?", userID).Find(&owned).Error; err != nil {
		return nil, err
	}
	out := make([]dashboardSet, 0, len(owned))
	seen := map[uint]bool{}
	for _, s := range owned {
		out = append(out, dashboardSet{s, RoleOwner})
		seen[s.ID] = true
	}
	var ms []model.TestSetMember
	if err := db.Where("user_id = ?", userID).Find(&ms).Error; err != nil {
		return nil, err
	}
	for _, m := range ms {
		if seen[m.TestSetID] {
			continue
		}
		var s model.TestSet
		if err := db.First(&s, m.TestSetID).Error; err != nil {
			continue
		}
		r := RoleRead
		if m.Role == model.RoleEdit {
			r = RoleEdit
		}
		out = append(out, dashboardSet{s, r})
		seen[s.ID] = true
	}
	return out, nil
}
func roleName(r RoleResult) string {
	switch r {
	case RoleOwner:
		return "owner"
	case RoleEdit:
		return "edit"
	case RoleRead:
		return "read"
	}
	return "none"
}
func uintString(v uint) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for v > 0 {
		b = append([]byte{digits[v%10]}, b...)
		v /= 10
	}
	return string(b)
}
