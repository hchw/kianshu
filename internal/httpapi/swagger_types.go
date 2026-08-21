package httpapi

import (
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"
)

// The types in this file exist purely for swagger documentation. They mirror
// the exact JSON shapes returned by the handlers, so the generated OpenAPI
// spec stays faithful to the real API contract.

// errorResp is the uniform error envelope returned on every failure path.
type errorResp struct {
	Error string `json:"error" example:"参数不合法"`
}

// okResp is returned by simple mutation endpoints.
type okResp struct {
	Ok bool `json:"ok" example:"true"`
}

// deletedResp is returned when a schedule is removed.
type deletedResp struct {
	Deleted bool `json:"deleted" example:"true"`
}

// registerResp is returned on successful account registration.
type registerResp struct {
	ID       uint   `json:"id" example:"1"`
	Username string `json:"username" example:"testuser"`
}

// loginResp is returned on successful login; token must be sent as
// `Authorization: Bearer <token>` on all authenticated endpoints.
type loginResp struct {
	Token    string `json:"token" example:"eyJhbGciOiJIUzI1NiIs..."`
	ID       uint   `json:"id" example:"1"`
	Username string `json:"username" example:"testuser"`
}

// testSetListResp is the list of test sets the caller owns or is a member of.
type testSetListResp struct {
	TestSets []model.TestSet `json:"test_sets"`
}

// importConfirmResp is returned with HTTP 422 when a non-standard swagger
// document needs explicit user confirmation before importing.
type importConfirmResp struct {
	NeedConfirmation bool     `json:"need_confirmation" example:"true"`
	Issues           []string `json:"issues" example:"[\"缺少 info.title\"]"`
	Error            string   `json:"error" example:"文档非标准"`
}

// unitListResp is the full test-unit listing.
type unitListResp struct {
	Units []model.TestUnit `json:"units"`
}

// unitBrief is the trimmed tool-facing view of a test unit.
type unitBrief struct {
	ID       uint   `json:"id" example:"1"`
	Method   string `json:"method" example:"GET"`
	Path     string `json:"path" example:"/users/{id}"`
	Slug     string `json:"slug" example:"get-users-{id}"`
	Tag      string `json:"tag" example:"用户"`
	Name     string `json:"name" example:"获取用户"`
	Security string `json:"security" example:"[{\"BearerAuth\":[]}]"`
}

// unitBriefListResp is the tool-facing test-unit listing.
type unitBriefListResp struct {
	Units []unitBrief `json:"units"`
}

// flowListResp is the list of flows inside a test set.
type flowListResp struct {
	Flows []model.TestFlow `json:"flows"`
}

// createFlowReq creates a new flow inside a test set.
type createFlowReq struct {
	Name string `json:"name" validate:"required" minLength:"1" example:"我的测试流"`
}

// duplicateFlowReq is the optional name override for a duplicated flow.
type duplicateFlowReq struct {
	Name string `json:"name" example:"我的测试流 副本"`
}

// renameFlowReq renames a flow.
type renameFlowReq struct {
	Name string `json:"name" validate:"required" minLength:"1" example:"支付流程"`
}

// draftView is the readable view of a flow's working draft.
type draftView struct {
	FlowID uint   `json:"flow_id" example:"1"`
	Name   string `json:"name" example:"我的测试流"`
	Tree   string `json:"tree" example:"{\"root\":{}}"`
}

// updateDraftReq updates a flow's working draft. tree may be an inline JSON
// object or a JSON-encoded string.
type updateDraftReq struct {
	Name string `json:"name" example:"我的测试流"`
	Tree any    `json:"tree" validate:"required"`
}

// versionListResp is the list of immutable snapshots of a flow.
type versionListResp struct {
	Versions []model.FlowVersion `json:"versions"`
}

// runListResp is the list of execution logs of a flow.
type runListResp struct {
	Runs []model.ExecutionLog `json:"runs"`
}

// scheduleListResp is the list of cron schedules of a flow.
type scheduleListResp struct {
	Schedules []model.FlowSchedule `json:"schedules"`
}

// createScheduleReq registers a cron schedule for a flow.
type createScheduleReq struct {
	Cron string `json:"cron" validate:"required" minLength:"1" example:"0 */2 * * *"`
}

// setScheduleEnabledReq toggles whether a schedule fires.
type setScheduleEnabledReq struct {
	Enabled bool `json:"enabled" example:"true"`
}

// providerListResp is the user's LLM provider listing (API keys stripped).
type providerListResp struct {
	Providers []providerView `json:"providers"`
}

// providerTestResp reports the outcome of a connectivity test.
type providerTestResp struct {
	Ok    bool   `json:"ok" example:"true"`
	Error string `json:"error,omitempty" example:"连接超时"`
}

// agentSessionView is the current dialog state of a flow.
type agentSessionView struct {
	Status           string                  `json:"status" example:"active"`
	Messages         []any                   `json:"messages"`
	PendingQuestions []service.PauseQuestion `json:"pending_questions"`
}

// agentNewResp reports a reset dialog session.
type agentNewResp struct {
	Ok        bool `json:"ok" example:"true"`
	SessionID uint `json:"session_id" example:"1"`
}

// pauseAnswerReq carries one answer to a paused generation question.
type pauseAnswerReq struct {
	QuestionID string `json:"question_id" validate:"required" example:"q1"`
	Answer     string `json:"answer" validate:"required" minLength:"1" example:"确认使用版本 v2"`
}

// memberListResp is the response for listing members.
type memberListResp struct {
	Owner   memberView   `json:"owner"`
	Members []memberView `json:"members"`
}

// userSearchResp is the response for searching users.
type userSearchResp struct {
	Users []userBrief `json:"users"`
}
