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
	Error string `json:"error"`
}

// okResp is returned by simple mutation endpoints.
type okResp struct {
	Ok bool `json:"ok"`
}

// deletedResp is returned when a schedule is removed.
type deletedResp struct {
	Deleted bool `json:"deleted"`
}

// registerResp is returned on successful account registration.
type registerResp struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

// loginResp is returned on successful login; token must be sent as
// `Authorization: Bearer <token>` on all authenticated endpoints.
type loginResp struct {
	Token    string `json:"token"`
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

// testSetListResp is the list of test sets the caller owns or is a member of.
type testSetListResp struct {
	TestSets []model.TestSet `json:"test_sets"`
}

// importConfirmResp is returned with HTTP 422 when a non-standard swagger
// document needs explicit user confirmation before importing.
type importConfirmResp struct {
	NeedConfirmation bool     `json:"need_confirmation"`
	Issues           []string `json:"issues"`
	Error            string   `json:"error"`
}

// unitListResp is the full test-unit listing.
type unitListResp struct {
	Units []model.TestUnit `json:"units"`
}

// unitBrief is the trimmed tool-facing view of a test unit.
type unitBrief struct {
	ID       uint   `json:"id"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Slug     string `json:"slug"`
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	Security string `json:"security"`
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
	Name string `json:"name"`
}

// draftView is the readable view of a flow's working draft.
type draftView struct {
	FlowID uint   `json:"flow_id"`
	Name   string `json:"name"`
	Tree   string `json:"tree"`
}

// updateDraftReq updates a flow's working draft. tree may be an inline JSON
// object or a JSON-encoded string.
type updateDraftReq struct {
	Name string `json:"name"`
	Tree any    `json:"tree"`
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
	Cron string `json:"cron"`
}

// setScheduleEnabledReq toggles whether a schedule fires.
type setScheduleEnabledReq struct {
	Enabled bool `json:"enabled"`
}

// providerListResp is the user's LLM provider listing (API keys stripped).
type providerListResp struct {
	Providers []providerView `json:"providers"`
}

// providerTestResp reports the outcome of a connectivity test.
type providerTestResp struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// agentSessionView is the current dialog state of a flow.
type agentSessionView struct {
	Status           string                  `json:"status"`
	Messages         []any                   `json:"messages"`
	PendingQuestions []service.PauseQuestion `json:"pending_questions"`
}

// agentNewResp reports a reset dialog session.
type agentNewResp struct {
	Ok        bool `json:"ok"`
	SessionID uint `json:"session_id"`
}

// pauseAnswerReq carries one answer to a paused generation question.
type pauseAnswerReq struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}
