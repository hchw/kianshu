package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type agentSubmitReq struct {
	ProviderID    uint     `json:"provider_id" validate:"required" example:"1"`
	Instruction   string   `json:"instruction" validate:"required" minLength:"1" example:"请为用户登录接口生成测试流"`
	SelectedNodes []string `json:"selected_nodes,omitempty" example:"[\"get-users-{id}\"]"`
	Mode          string   `json:"mode" enum:"edit,generate" example:"edit"`
}

type agentResumeReq struct {
	ProviderID uint                  `json:"provider_id" validate:"required" example:"1"`
	Answers    []service.PauseAnswer `json:"answers" validate:"required"`
}

// agentProvider loads and decrypts the user's LLM provider for an agent run.
func (s *Server) agentProvider(c *gin.Context, providerID uint) (service.ChatProvider, bool) {
	uid, _ := userIDOf(c)
	var p model.Provider
	if err := s.DB.Where("id = ? AND user_id = ?", providerID, uid).First(&p).Error; err != nil {
		writeErr(c, http.StatusNotFound, "Provider 不存在")
		return nil, false
	}
	if !p.Enabled {
		writeErr(c, http.StatusBadRequest, "Provider 未启用")
		return nil, false
	}
	key, err := s.Cipher.Decrypt(p.APIKeyEnc)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "密钥解密失败")
		return nil, false
	}
	if s.AgentLLM != nil {
		return s.AgentLLM, true
	}
	return &agentLLM{Client: s.LLM, cfg: &providerAdapter{p.BaseURL, key, p.Model}}, true
}

// agentLLM combines the shared OpenAI-compatible client with one user provider's
// config so it satisfies service.ChatProvider.
type agentLLM struct {
	*openai.Client
	cfg *providerAdapter
}

func (a *agentLLM) GetBaseURL() string { return a.cfg.GetBaseURL() }
func (a *agentLLM) GetAPIKey() string  { return a.cfg.GetAPIKey() }
func (a *agentLLM) GetModel() string   { return a.cfg.GetModel() }

// handleAgentSubmit runs one agent submission. With Accept: text/event-stream
// it streams each tool round as SSE; otherwise it returns the collected events.
// handleAgentSubmit runs one agent submission. With Accept: text/event-stream
// it streams each tool round as SSE; otherwise it returns the collected events.
//
//	@Summary	提交 LLM Agent 指令(生成/编辑)
//	@Description	向 LLM Agent 提交一次指令,按需生成或编辑测试流。请求头 Accept 含 text/event-stream 时以 SSE 逐轮推送工具事件,否则返回聚合结果。mode 为 edit(默认)或 generate。
//	@Tags		LLM Agent
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint			true	"流 ID"
//	@Param		body	body	agentSubmitReq	true	"提交内容(provider_id 与 instruction 必填)"
//	@Success	200	{object}	service.AgentResult	"Agent 执行结果(events 为各轮工具事件)"
//	@Failure	400	{object}	errorResp	"参数不合法 / Provider 未启用"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	404	{object}	errorResp	"Provider 或流不存在"
//	@Failure	409	{object}	errorResp	"该流已有正在进行的提交"
//	@Failure	500	{object}	errorResp	"执行失败"
//	@Router		/flow/flows/{flowID}/agent/submit [post]
func (s *Server) handleAgentSubmit(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req agentSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProviderID == 0 || req.Instruction == "" {
		writeErr(c, http.StatusBadRequest, "provider_id 与 instruction 必填")
		return
	}
	provider, ok := s.agentProvider(c, req.ProviderID)
	if !ok {
		return
	}
	mode := service.ModeEdit
	if req.Mode == "generate" {
		mode = service.ModeGenerate
	}

	unlock := service.LockFlow(flowID)
	if unlock == nil {
		writeErr(c, http.StatusConflict, service.ErrSessionBusy.Error())
		return
	}

	if isSSE(c) {
		// For SSE the unlock moves into the streaming goroutine so it is never
		// released while the generation loop is still running after the client
		// disconnects. The non-SSE path below releases it synchronously.
		s.submitSSE(c, flowID, req, provider, mode, unlock)
		return
	}
	defer unlock()

	// Non-SSE: collect events and return them with the result.
	var events []service.Event
	result, err := s.runAgent(c, flowID, req, provider, mode, func(ev service.Event) {
		events = append(events, ev)
	})
	if err != nil {
		if errors.Is(err, service.ErrFlowNotFound) {
			writeErr(c, http.StatusNotFound, "流不存在")
			return
		}
		writeErr(c, http.StatusInternalServerError, "执行失败: "+err.Error())
		return
	}
	if len(events) > 0 {
		result.Events = events
	}
	writeJSON(c, http.StatusOK, result)
}

// runAgent dispatches to generate or edit mode.
func (s *Server) runAgent(c *gin.Context, flowID uint, req agentSubmitReq, provider service.ChatProvider, mode service.Mode, emit func(service.Event)) (*service.AgentResult, error) {
	if mode == service.ModeGenerate {
		return service.GenerateFlow(c.Request.Context(), s.DB, flowID, currentUserID(c), req.Instruction, provider)
	}
	return service.RunAgent(c.Request.Context(), s.DB, flowID, currentUserID(c), service.AgentOptions{
		Provider:      provider,
		Instruction:   req.Instruction,
		SelectedNodes: req.SelectedNodes,
		Mode:          mode,
		Emit:          emit,
	})
}

// submitSSE streams each tool round over Server-Sent Events. The generation
// loop runs in a goroutine that watches c.Request.Context(): when the client
// disconnects the loop stops promptly, event sends never block on a closed
// stream, and the session lock is released only after the loop truly ends.
func (s *Server) submitSSE(c *gin.Context, flowID uint, req agentSubmitReq, provider service.ChatProvider, mode service.Mode, unlock func()) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()

	ctx := c.Request.Context()
	stream := make(chan service.Event, 64)
	done := make(chan error, 1)
	go func() {
		defer unlock()
		_, err := s.runAgent(c, flowID, req, provider, mode, func(ev service.Event) {
			select {
			case stream <- ev:
			case <-ctx.Done():
			}
		})
		// Buffered, so this send never blocks even if the stream loop has
		// already returned on disconnect.
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
					log.Errorf("agent: submitSSE flow=%d 执行失败: %v", flowID, err)
					io.WriteString(w, "event: error\ndata: "+jsonString(err.Error())+"\n\n")
				}
			io.WriteString(w, "data: [DONE]\n\n")
			return false
		}
	})
}

func currentUserID(c *gin.Context) uint {
	uid, _ := userIDOf(c)
	return uid
}

func isSSE(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/event-stream")
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// handleAgentSession returns the dialog state of a flow.
//
//	@Summary	获取 Agent 会话
//	@Description	返回流的 LLM 会话状态:status(active/paused)、历史消息与待回答的暂停问题。需要读权限。
//	@Tags		LLM Agent
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	agentSessionView	"会话状态"
//	@Failure	400	{object}	errorResp	"无效的流 ID"
//	@Failure	403	{object}	errorResp	"无权访问该流"
//	@Failure	500	{object}	errorResp	"读取会话失败"
//	@Router		/flow/flows/{flowID}/agent/session [get]
func (s *Server) handleAgentSession(c *gin.Context) {
	flowID, _, ok := s.flowReadable(c)
	if !ok {
		return
	}
	sess, err := service.GetFlowSession(s.DB, flowID, currentUserID(c))
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "读取会话失败")
		return
	}
	var msgs []any
	_ = json.Unmarshal([]byte(sess.Messages), &msgs)
	var questions []service.PauseQuestion
	if sess.PendingQuestions != "" {
		_ = json.Unmarshal([]byte(sess.PendingQuestions), &questions)
	}
	writeJSON(c, http.StatusOK, gin.H{
		"status":            sess.Status,
		"messages":          msgs,
		"pending_questions": questions,
	})
}

// handleAgentResume resumes a paused generation with the user's answers.
//
//	@Summary	恢复 Agent 生成(回答暂停问题)
//	@Description	当生成流程在冲突/暂停点停下时,提交对暂停问题的回答以继续。需要编辑权限。
//	@Tags		LLM Agent
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint			true	"流 ID"
//	@Param		body	body	agentResumeReq	true	"provider_id 与 answers 必填"
//	@Success	200	{object}	service.AgentResult	"恢复执行结果"
//	@Failure	400	{object}	errorResp	"参数不合法 / Provider 未启用"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	404	{object}	errorResp	"Provider 不存在"
//	@Failure	409	{object}	errorResp	"该流已有正在进行的提交"
//	@Failure	500	{object}	errorResp	"继续生成失败"
//	@Router		/flow/flows/{flowID}/agent/resume [post]
func (s *Server) handleAgentResume(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	var req agentResumeReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProviderID == 0 || len(req.Answers) == 0 {
		writeErr(c, http.StatusBadRequest, "provider_id 与 answers 必填")
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
	defer unlock()
	result, err := service.ResumeGeneration(c.Request.Context(), s.DB, flowID, currentUserID(c), req.Answers, provider)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "继续生成失败: "+err.Error())
		return
	}
	writeJSON(c, http.StatusOK, result)
}

// handleAgentNew resets a flow's dialog session.
//
//	@Summary	重置 Agent 会话
//	@Description	清空流的 LLM 会话历史,开启全新会话。需要编辑权限。
//	@Tags		LLM Agent
//	@Produce	json
//	@Security	BearerAuth
//	@Param		flowID	path	uint	true	"流 ID"
//	@Success	200	{object}	agentNewResp	"新会话 ID"
//	@Failure	400	{object}	errorResp	"无效的流 ID"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	500	{object}	errorResp	"重置会话失败"
//	@Router		/flow/flows/{flowID}/agent/new [post]
func (s *Server) handleAgentNew(c *gin.Context) {
	flowID, _, ok := s.flowEditable(c)
	if !ok {
		return
	}
	sess, err := service.ResetFlowSession(s.DB, flowID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "重置会话失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "session_id": sess.ID})
}
