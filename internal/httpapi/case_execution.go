package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type caseGenerateReq struct {
	ProviderID  uint                    `json:"provider_id"`
	Selections  []service.CaseSelection `json:"selections"`
	Instruction string                  `json:"instruction,omitempty"`
}

// handleGenerateFlowFromCases 从多个用例子树生成执行流。
func (s *Server) handleGenerateFlowFromCases(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req caseGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProviderID == 0 || len(req.Selections) == 0 {
		writeErr(c, http.StatusBadRequest, "provider_id 与 selections 必填")
		return
	}
	provider, ok := s.agentProvider(c, req.ProviderID)
	if !ok {
		return
	}
	unlock := service.LockFlow(flowID)
	if unlock == nil {
		writeErr(c, http.StatusConflict, service.ErrSessionBusy.Error())
		return
	}
	if isSSE(c) {
		s.caseGenerateSSE(c, flowID, req, provider, unlock)
		return
	}
	defer unlock()
	var events []service.Event
	_, err := service.GenerateFlowFromCases(c.Request.Context(), s.DB, flowID, currentUserID(c), req.Selections, provider, service.AgentHooks{Emit: func(ev service.Event) {
		events = append(events, ev)
	}})
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "生成失败: "+err.Error())
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"events": events})
}

func (s *Server) caseGenerateSSE(c *gin.Context, flowID uint, req caseGenerateReq, provider service.ChatProvider, unlock func()) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()
	ctx := c.Request.Context()
	stream := make(chan service.Event, 64)
	done := make(chan error, 1)
	go func() {
		defer unlock()
		_, err := service.GenerateFlowFromCases(ctx, s.DB, flowID, currentUserID(c), req.Selections, provider, service.AgentHooks{Emit: func(ev service.Event) {
			select {
			case stream <- ev:
			case <-ctx.Done():
			}
		}})
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
				log.Errorf("case generate flow=%d 执行失败: %v", flowID, err)
			}
			io.WriteString(w, "data: [DONE]\n\n")
			return false
		}
	})
}

// handleSaveFlowFromCases 保存执行流版本并原子建立覆盖关联。
func (s *Server) handleSaveFlowFromCases(c *gin.Context) {
	flowID, uid, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req struct {
		Selections []service.CaseSelection `json:"selections"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Selections) == 0 {
		writeErr(c, http.StatusBadRequest, "selections 必填")
		return
	}
	version, err := service.SaveExecutionFlowFromCases(s.DB, flowID, uid, req.Selections)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "保存失败: "+err.Error())
		return
	}
	writeJSON(c, http.StatusCreated, version)
}
