package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github/hchw/kianshu/internal/agent"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	"gorm.io/gorm"
)

// ErrSessionBusy indicates another agent submission is already running for the
// flow.
var ErrSessionBusy = errors.New("该流已有正在进行的提交,请稍后再试")

// ErrFlowNotFound re-exported alias for callers that only import service.

// sessionLocks guards concurrent agent submissions per flow.
var sessionLocks = agent.NewKeyedLock()

// LockFlow serializes agent submissions for one flow. It returns an unlock
// function, or nil when another submission is already in progress.
func LockFlow(flowID uint) func() {
	return sessionLocks.TryLock(flowID)
}

// GetFlowSession returns the dialog session of a flow, creating one if absent.
func GetFlowSession(db *gorm.DB, flowID, userID uint) (*model.FlowSession, error) {
	var s model.FlowSession
	err := db.Where("flow_id = ?", flowID).Order("id").First(&s).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s = model.FlowSession{FlowID: flowID, CreatedBy: userID, Status: model.SessionActive, Messages: "[]"}
	if err := db.Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ResetFlowSession clears a flow's dialog history (new session).
func ResetFlowSession(db *gorm.DB, flowID uint) (*model.FlowSession, error) {
	s, err := GetFlowSession(db, flowID, 0)
	if err != nil {
		return nil, err
	}
	s.Messages = "[]"
	s.PendingQuestions = ""
	s.Status = model.SessionActive
	if err := db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// SaveFlowSession persists the messages and pause state of a session.
func SaveFlowSession(db *gorm.DB, s *model.FlowSession) error {
	return db.Save(s).Error
}

// unmarshalSessionMessages decodes the stored message array.
func unmarshalSessionMessages(s *model.FlowSession) ([]openai.Message, error) {
	if s == nil || s.Messages == "" {
		return []openai.Message{}, nil
	}
	var msgs []openai.Message
	if err := json.Unmarshal([]byte(s.Messages), &msgs); err != nil {
		return nil, fmt.Errorf("解析会话消息失败: %w", err)
	}
	return fixMessageHistory(msgs), nil
}

// fixMessageHistory detects and fixes trailing incomplete assistant(tool_calls)
// messages that lack matching tool responses. This can happen when a scope-
// violation pause saved the session mid-round (before this was fixed).
func fixMessageHistory(msgs []openai.Message) []openai.Message {
	return agent.FixMessageHistory(msgs)
}

// CompressSession compacts a flow's dialog history by keeping the system
// prompt, a summary of tool operations, and the last user message, dropping
// the bulky intermediate tool call/result pairs to save tokens.
func CompressSession(db *gorm.DB, flowID uint) (*model.FlowSession, error) {
	s, err := GetFlowSession(db, flowID, 0)
	if err != nil {
		return nil, err
	}
	msgs, err := unmarshalSessionMessages(s)
	if err != nil {
		return nil, err
	}
	if len(msgs) <= 2 {
		return s, nil // nothing to compress
	}

	// Keep the existing flow-specific summary wording while delegating the
	// message selection and compaction mechanics to the shared runtime.
	compressed := agent.CompactMessages(msgs, func(ops []string) string {
		return fmt.Sprintf("之前对话中执行了 %d 次工具调用（%s）。当前流草稿即这些操作的结果。请据此继续。",
			len(ops), strings.Join(dedupeSlice(ops), ", "))
	})

	b, err := json.Marshal(compressed)
	if err != nil {
		return nil, err
	}
	s.Messages = string(b)
	if err := db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// dedupeSlice removes consecutive duplicates from a string slice, preserving order.
func dedupeSlice(xs []string) []string {
	if len(xs) == 0 {
		return xs
	}
	out := []string{xs[0]}
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
