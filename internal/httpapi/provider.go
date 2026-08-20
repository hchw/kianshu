package httpapi

import (
	"context"
	"net/http"

	"github/hchw/kianshu/internal/model"

	"github.com/gin-gonic/gin"
)

type providerReq struct {
	Name    string `json:"name" validate:"required" minLength:"1" example:"我的 OpenAI"`
	BaseURL string `json:"base_url" validate:"required" minLength:"1" format:"uri" example:"https://api.openai.com"`
	APIKey  string `json:"api_key" example:"sk-xxx"`
	Model   string `json:"model" example:"gpt-4"`
	// StrictContent 为 true 时,client 把 null content 改为空串,兼容 ollama/vLLM。
	StrictContent bool `json:"strict_content" example:"false"`
	Enabled *bool  `json:"enabled" example:"true"`
}

// providerView strips sensitive fields for responses.
type providerView struct {
	ID      uint   `json:"id" example:"1"`
	Name    string `json:"name" example:"我的 OpenAI"`
	BaseURL string `json:"base_url" example:"https://api.openai.com"`
	Model   string `json:"model" example:"gpt-4"`
	StrictContent bool   `json:"strict_content" example:"false"`
	Enabled bool   `json:"enabled" example:"true"`
}

func toView(p model.Provider) providerView {
	return providerView{p.ID, p.Name, p.BaseURL, p.Model, p.StrictContent, p.Enabled}
}

// handleListProviders lists the caller's LLM providers (API keys stripped).
//
//	@Summary	列出 LLM Provider
//	@Description	返回当前用户配置的全部 LLM Provider,响应中不包含 API 密钥。
//	@Tags		LLM Provider
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	providerListResp	"Provider 列表"
//	@Router		/providers [get]
func (s *Server) handleListProviders(c *gin.Context) {
	uid, _ := userIDOf(c)
	var rows []model.Provider
	s.DB.Where("user_id = ?", uid).Order("id").Find(&rows)
	out := make([]providerView, 0, len(rows))
	for _, p := range rows {
		out = append(out, toView(p))
	}
	writeJSON(c, http.StatusOK, gin.H{"providers": out})
}

// handleCreateProvider creates a user-scoped LLM provider.
//
//	@Summary	创建 LLM Provider
//	@Description	创建兼容 OpenAI 协议的 LLM Provider。name 与 base_url 必填;api_key 会被加密存储,响应不回显。默认启用。
//	@Tags		LLM Provider
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body	providerReq	true	"Provider 配置"
//	@Success	201	{object}	providerView	"创建的 Provider(不含 api_key)"
//	@Failure	400	{object}	errorResp		"name 与 base_url 必填"
//	@Failure	500	{object}	errorResp		"密钥加密失败 / 创建失败"
//	@Router		/providers [post]
func (s *Server) handleCreateProvider(c *gin.Context) {
	uid, _ := userIDOf(c)
	var req providerReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.BaseURL == "" {
		writeErr(c, http.StatusBadRequest, "name 与 base_url 必填")
		return
	}
	enc, err := s.Cipher.Encrypt(req.APIKey)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "密钥加密失败")
		return
	}
	p := model.Provider{
		UserID:    uid,
		Name:      req.Name,
		BaseURL:   req.BaseURL,
		APIKeyEnc: enc,
		Model:         req.Model,
		StrictContent: req.StrictContent,
		Enabled:       true,
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if err := s.DB.Create(&p).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(c, http.StatusCreated, toView(p))
}

// handleGetProvider returns one of the caller's LLM providers.
//
//	@Summary	获取 LLM Provider
//	@Description	返回指定 Provider 的配置(不含 API 密钥)。
//	@Tags		LLM Provider
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	uint	true	"Provider ID"
//	@Success	200	{object}	providerView	"Provider 配置"
//	@Failure	400	{object}	errorResp		"无效的 Provider ID"
//	@Failure	404	{object}	errorResp		"Provider 不存在"
//	@Router		/providers/{id} [get]
func (s *Server) handleGetProvider(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的 Provider ID")
		return
	}
	var p model.Provider
	if err := s.DB.Where("id = ? AND user_id = ?", id, uid).First(&p).Error; err != nil {
		writeErr(c, http.StatusNotFound, "Provider 不存在")
		return
	}
	writeJSON(c, http.StatusOK, toView(p))
}

// handleUpdateProvider updates a user's LLM provider (partial fields).
//
//	@Summary	更新 LLM Provider
//	@Description	更新 Provider 的字段,仅更新传入字段。api_key 传入时重新加密存储。
//	@Tags		LLM Provider
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint			true	"Provider ID"
//	@Param		body	body	providerReq	true	"更新内容"
//	@Success	200	{object}	providerView	"更新后的 Provider"
//	@Failure	400	{object}	errorResp		"无效的 ID / 请求体不合法"
//	@Failure	404	{object}	errorResp		"Provider 不存在"
//	@Failure	500	{object}	errorResp		"更新失败"
//	@Router		/providers/{id} [patch]
func (s *Server) handleUpdateProvider(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的 Provider ID")
		return
	}
	var p model.Provider
	if err := s.DB.Where("id = ? AND user_id = ?", id, uid).First(&p).Error; err != nil {
		writeErr(c, http.StatusNotFound, "Provider 不存在")
		return
	}
	var req providerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.BaseURL != "" {
		p.BaseURL = req.BaseURL
	}
	if req.APIKey != "" {
		enc, err := s.Cipher.Encrypt(req.APIKey)
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "密钥加密失败")
			return
		}
		p.APIKeyEnc = enc
	}
	if req.Model != "" {
		p.Model = req.Model
	}
	p.StrictContent = req.StrictContent
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if err := s.DB.Save(&p).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(c, http.StatusOK, toView(p))
}

// handleDeleteProvider removes a user's LLM provider.
//
//	@Summary	删除 LLM Provider
//	@Description	删除指定 Provider 配置。
//	@Tags		LLM Provider
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	uint	true	"Provider ID"
//	@Success	200	{object}	okResp		"已删除"
//	@Failure	400	{object}	errorResp	"无效的 Provider ID"
//	@Failure	404	{object}	errorResp	"Provider 不存在"
//	@Router		/providers/{id} [delete]
func (s *Server) handleDeleteProvider(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的 Provider ID")
		return
	}
	res := s.DB.Where("id = ? AND user_id = ?", id, uid).Delete(&model.Provider{})
	if res.RowsAffected == 0 {
		writeErr(c, http.StatusNotFound, "Provider 不存在")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}

type providerAdapter struct {
	baseURL       string
	apiKey        string
	model         string
	strictContent bool
}

func (p *providerAdapter) GetBaseURL() string     { return p.baseURL }
func (p *providerAdapter) GetAPIKey() string      { return p.apiKey }
func (p *providerAdapter) GetModel() string       { return p.model }
func (p *providerAdapter) GetStrictContent() bool { return p.strictContent }

// handleTestProvider verifies connectivity to an LLM provider.
//
//	@Summary	测试 Provider 连通性
//	@Description	使用配置的 base_url / api_key / model 发起一次连通性测试。ok=false 时 error 字段给出原因。
//	@Tags		LLM Provider
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	uint	true	"Provider ID"
//	@Success	200	{object}	providerTestResp	"测试结果(ok 为是否连通,失败时含 error)"
//	@Failure	400	{object}	errorResp	"无效的 Provider ID"
//	@Failure	404	{object}	errorResp	"Provider 不存在"
//	@Failure	500	{object}	errorResp	"密钥解密失败"
//	@Router		/providers/{id}/test [post]
func (s *Server) handleTestProvider(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的 Provider ID")
		return
	}
	var p model.Provider
	if err := s.DB.Where("id = ? AND user_id = ?", id, uid).First(&p).Error; err != nil {
		writeErr(c, http.StatusNotFound, "Provider 不存在")
		return
	}
	key, err := s.Cipher.Decrypt(p.APIKeyEnc)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "密钥解密失败")
		return
	}
	adapter := &providerAdapter{p.BaseURL, key, p.Model, p.StrictContent}
	if err := s.LLM.Test(context.Background(), adapter); err != nil {
		writeJSON(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}
