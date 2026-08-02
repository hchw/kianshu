package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"

	"gorm.io/gorm"
)

// ErrSessionBusy indicates another agent submission is already running for the
// flow.
var ErrSessionBusy = errors.New("该流已有正在进行的提交,请稍后再试")

// ErrFlowNotFound re-exported alias for callers that only import service.

// sessionLocks guards concurrent agent submissions per flow.
var sessionLocks = struct {
	sync.Mutex
	held map[uint]*sync.Mutex
}{held: map[uint]*sync.Mutex{}}

// LockFlow serializes agent submissions for one flow. It returns an unlock
// function, or nil when another submission is already in progress.
func LockFlow(flowID uint) func() {
	sessionLocks.Lock()
	m, ok := sessionLocks.held[flowID]
	if !ok {
		m = &sync.Mutex{}
		sessionLocks.held[flowID] = m
	}
	sessionLocks.Unlock()
	if !m.TryLock() {
		return nil
	}
	return m.Unlock
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
	return msgs, nil
}
