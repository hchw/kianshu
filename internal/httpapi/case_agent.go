package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type caseAgentSubmitReq struct {
	ProviderID  uint   `json:"provider_id"`
	Instruction string `json:"instruction"`
	Thinking    string `json:"thinking,omitempty"`
}

type caseAgentResumeReq struct {
	ProviderID uint                  `json:"provider_id"`
	Answers    []service.PauseAnswer `json:"answers"`
	Thinking   string                `json:"thinking,omitempty"`
}

// handleCaseAgentSubmit runs one case flow agent submission (SSE or JSON).
func (s *Server) handleCaseAgentSubmit(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var req caseAgentSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProviderID == 0 || req.Instruction == "" {
		writeErr(c, http.StatusBadRequest, "provider_id 与 instruction 必填")
		return
	}
	provider, ok := s.agentProvider(c, req.ProviderID)
	if !ok {
		return
	}
	unlock := service.LockCaseFlow(cfID)
	if unlock == nil {
		writeErr(c, http.StatusConflict, service.ErrSessionBusy.Error())
		return
	}
	if isSSE(c) {
		s.submitCaseSSE(c, cfID, req, provider, unlock)
		return
	}
	defer unlock()
	var events []service.Event
	result, err := service.RunCaseAgent(c.Request.Context(), s.DB, cfID, currentUserID(c), service.CaseAgentOptions{
		Provider: provider, Instruction: req.Instruction, Thinking: req.Thinking,
		Emit: func(ev service.Event) { events = append(events, ev) },
	})
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "执行失败: "+err.Error())
		return
	}
	toolEvents := events[:0]
	for _, ev := range events {
		if ev.Kind == "" || ev.Kind == service.EventKindTool {
			toolEvents = append(toolEvents, ev)
		}
	}
	if len(toolEvents) > 0 {
		result.Events = toolEvents
	}
	writeJSON(c, http.StatusOK, result)
}

func (s *Server) submitCaseSSE(c *gin.Context, cfID uint, req caseAgentSubmitReq, provider service.ChatProvider, unlock func()) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()
	ctx := c.Request.Context()
	stream := make(chan service.Event, 64)
	done := make(chan error, 1)
	go func() {
		defer unlock()
		_, err := service.RunCaseAgent(ctx, s.DB, cfID, currentUserID(c), service.CaseAgentOptions{
			Provider: provider, Instruction: req.Instruction, Thinking: req.Thinking,
			Emit: func(ev service.Event) {
				select {
				case stream <- ev:
				case <-ctx.Done():
				}
			},
		})
		done <- err
	}()
	c.Stream(func(w io.Writer) bool {
		select {
		case ev := <-stream:
			data, _ := json.Marshal(ev)
			io.WriteString(w, "data: "+string(data)+"\n\n")
			return true
		case err := <-done:
			if err != nil {
				log.Errorf("case agent: case_flow=%d 执行失败: %v", cfID, err)
			}
			io.WriteString(w, "data: [DONE]\n\n")
			return false
		}
	})
}

// handleCaseAgentSession returns the case flow dialog session.
func (s *Server) handleCaseAgentSession(c *gin.Context) {
	cfID, uid, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	session, err := service.GetCaseFlowSession(s.DB, cfID, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询会话失败")
		return
	}
	writeJSON(c, http.StatusOK, session)
}

// handleCaseAgentResume resumes a paused case flow conversation.
func (s *Server) handleCaseAgentResume(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var req caseAgentResumeReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProviderID == 0 || len(req.Answers) == 0 {
		writeErr(c, http.StatusBadRequest, "provider_id 与 answers 必填")
		return
	}
	provider, ok := s.agentProvider(c, req.ProviderID)
	if !ok {
		return
	}
	unlock := service.LockCaseFlow(cfID)
	if unlock == nil {
		writeErr(c, http.StatusConflict, service.ErrSessionBusy.Error())
		return
	}
	if isSSE(c) {
		s.resumeCaseSSE(c, cfID, req, provider, unlock)
		return
	}
	defer unlock()
	var events []service.Event
	result, err := service.ResumeCaseAgent(c.Request.Context(), s.DB, cfID, currentUserID(c), req.Answers, provider, func(ev service.Event) {
		events = append(events, ev)
	})
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "继续生成失败: "+err.Error())
		return
	}
	writeJSON(c, http.StatusOK, result)
}

func (s *Server) resumeCaseSSE(c *gin.Context, cfID uint, req caseAgentResumeReq, provider service.ChatProvider, unlock func()) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()
	ctx := c.Request.Context()
	stream := make(chan service.Event, 64)
	done := make(chan error, 1)
	go func() {
		defer unlock()
		_, err := service.ResumeCaseAgent(ctx, s.DB, cfID, currentUserID(c), req.Answers, provider, func(ev service.Event) {
			select {
			case stream <- ev:
			case <-ctx.Done():
			}
		})
		done <- err
	}()
	c.Stream(func(w io.Writer) bool {
		select {
		case ev := <-stream:
			data, _ := json.Marshal(ev)
			io.WriteString(w, "data: "+string(data)+"\n\n")
			return true
		case err := <-done:
			if err != nil {
				log.Errorf("case agent resume: case_flow=%d 执行失败: %v", cfID, err)
			}
			io.WriteString(w, "data: [DONE]\n\n")
			return false
		}
	})
}

// handleCaseAgentNew resets a case flow dialog session.
func (s *Server) handleCaseAgentNew(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	sess, err := service.ResetCaseFlowSession(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "重置会话失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "session_id": sess.ID})
}

// handleCaseAgentCompress compacts a case flow dialog session.
func (s *Server) handleCaseAgentCompress(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	sess, err := service.CompressCaseSession(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "压缩会话失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "session_id": sess.ID})
}
