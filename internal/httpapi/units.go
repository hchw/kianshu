package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

type importReq struct {
	Source  string          `json:"source" example:"my-api"`
	Content json.RawMessage `json:"content" swaggertype:"object" validate:"required"`
	Confirm bool            `json:"confirm" example:"true"`
}

// handleImport imports a swagger document into a test set, deriving test units.
//
//	@Summary	导入 Swagger
//	@Description	将 Swagger 2.0 / OpenAPI 3.x 文档导入测试集,按 method+path 去重生成/更新测试单元。content 可为 swagger JSON 对象或 JSON 字符串;source 为来源标识(可选)。若文档非标准且 confirm 为 false,返回 422 需用户确认。
//	@Tags		测试单元
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		uint					true	"测试集 ID"
//	@Param		body	body		importReq				true	"导入内容"
//	@Success	201		{object}	service.ImportResult	"导入成功(created/updated 为新增与更新数量)"
//	@Failure	400		{object}	errorResp				"content 不合法 / 文档无法解析导入"
//	@Failure	403		{object}	errorResp				"无编辑权限"
//	@Failure	422		{object}	importConfirmResp		"文档非标准,需 confirm=true 后重试"
//	@Failure	500		{object}	errorResp				"导入失败"
//	@Router		/test-sets/{id}/imports [post]
func (s *Server) handleImport(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	canEdit, err := service.CanEdit(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return
	}
	var req importReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	data, err := importContent(req.Content)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "content 必须是 swagger JSON 字符串或对象")
		return
	}
	res, err := service.ImportSwagger(s.DB, id, req.Source, data, req.Confirm)
	if err != nil {
		var nce *service.NeedConfirmationError
		if errors.As(err, &nce) {
			writeJSON(c, http.StatusUnprocessableEntity, gin.H{
				"need_confirmation": true,
				"issues":            nce.Issues,
				"error":             nce.Error(),
			})
			return
		}
		if errors.Is(err, service.ErrInvalidDocument) {
			writeErr(c, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(c, http.StatusInternalServerError, "导入失败")
		return
	}
	writeJSON(c, http.StatusCreated, res)
}

func importContent(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errors.New("empty content")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return []byte(trimmed), nil
}

// requireReadAccess validates the caller can read the test set and returns its ID.
func (s *Server) requireReadAccess(c *gin.Context) (uint, bool) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return 0, false
	}
	canRead, err := service.CanRead(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, false
	}
	if !canRead {
		writeErr(c, http.StatusForbidden, "无权访问该测试集")
		return 0, false
	}
	return id, true
}

// handleListUnits lists test units with optional filters.
//
//	@Summary	查询测试单元
//	@Description	按测试集列出测试单元,支持 tag / method / 关键字(q,匹配 name、path、slug)过滤;include_deleted=true 时包含已软删单元。需要读权限。
//	@Tags		测试单元
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id				path	uint	true	"测试集 ID"
//	@Param		tag				query	string	false	"按 tag 过滤"
//	@Param		method			query	string	false	"按请求方法过滤(GET/POST/...)"
//	@Param		q				query	string	false	"关键字,匹配名称/路径/slug"
//	@Param		include_deleted	query	bool	false	"是否包含已软删单元,默认 false"
//	@Success	200	{object}	unitListResp	"测试单元列表"
//	@Failure	400	{object}	errorResp	"无效的测试集 ID"
//	@Failure	403	{object}	errorResp	"无权访问该测试集"
//	@Failure	500	{object}	errorResp	"查询失败"
//	@Router		/test-sets/{id}/units [get]
func (s *Server) handleListUnits(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	q := c.Request.URL.Query()
	db := s.DB
	if q.Get("include_deleted") == "true" {
		db = db.Unscoped()
	}
	query := db.Model(&model.TestUnit{}).Where("test_set_id = ?", setID)
	if tag := q.Get("tag"); tag != "" {
		query = query.Where("tag = ?", tag)
	}
	if method := strings.ToUpper(q.Get("method")); method != "" {
		query = query.Where("method = ?", method)
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		like := "%" + kw + "%"
		query = query.Where("name LIKE ? OR path LIKE ? OR slug LIKE ?", like, like, like)
	}
	var units []model.TestUnit
	if err := query.Order("slug").Find(&units).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"units": units})
}

// handleToolListUnits returns a trimmed unit listing for LLM agent tool calls.
//
//	@Summary	查询测试单元(工具精简版)
//	@Description	供 LLM agent 工具使用的精简单元列表,最多 500 条,不含软删单元。支持 tag / 关键字 q 过滤。
//	@Tags		测试单元
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint	true	"测试集 ID"
//	@Param		tag		query	string	false	"按 tag 过滤"
//	@Param		q		query	string	false	"关键字,匹配名称/路径/slug"
//	@Success	200	{object}	unitBriefListResp	"精简单元列表"
//	@Failure	400	{object}	errorResp			"无效的测试集 ID"
//	@Failure	403	{object}	errorResp			"无权访问该测试集"
//	@Failure	500	{object}	errorResp			"查询失败"
//	@Router		/test-sets/{id}/units/tool-list [get]
func (s *Server) handleToolListUnits(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	q := c.Request.URL.Query()
	query := s.DB.Model(&model.TestUnit{}).Where("test_set_id = ? AND deleted_at IS NULL", setID)
	if tag := q.Get("tag"); tag != "" {
		query = query.Where("tag = ?", tag)
	}
	if kw := strings.TrimSpace(q.Get("q")); kw != "" {
		like := "%" + kw + "%"
		query = query.Where("name LIKE ? OR path LIKE ? OR slug LIKE ?", like, like, like)
	}
	var units []model.TestUnit
	if err := query.Order("slug").Limit(500).Find(&units).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	type brief struct {
		ID       uint   `json:"id"`
		Method   string `json:"method"`
		Path     string `json:"path"`
		Slug     string `json:"slug"`
		Tag      string `json:"tag"`
		Name     string `json:"name"`
		Security string `json:"security"`
	}
	out := make([]brief, 0, len(units))
	for _, u := range units {
		out = append(out, brief{u.ID, u.Method, u.Path, u.Slug, u.Tag, u.Name, u.Security})
	}
	writeJSON(c, http.StatusOK, gin.H{"units": out})
}

// handleGetUnit returns one test unit with its full redundant interface info.
//
//	@Summary	获取测试单元
//	@Description	返回单个测试单元的完整信息(含请求/响应/安全契约的原始 JSON)。需要读权限。
//	@Tags		测试单元
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint	true	"测试集 ID"
//	@Param		unitID	path	uint	true	"测试单元 ID"
//	@Success	200	{object}	model.TestUnit	"测试单元"
//	@Failure	400	{object}	errorResp		"无效的 ID"
//	@Failure	403	{object}	errorResp		"无权访问该测试集"
//	@Failure	404	{object}	errorResp		"单元不存在"
//	@Router		/test-sets/{id}/units/{unitID} [get]
func (s *Server) handleGetUnit(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	unitID, ok := parseID(c, "unitID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的单元 ID")
		return
	}
	var unit model.TestUnit
	if err := s.DB.Where("id = ? AND test_set_id = ?", unitID, setID).First(&unit).Error; err != nil {
		writeErr(c, http.StatusNotFound, "单元不存在")
		return
	}
	writeJSON(c, http.StatusOK, unit)
}

// handleDeleteUnit soft-deletes a test unit so historical flow versions keep working.
//
//	@Summary	删除测试单元(软删)
//	@Description	软删除指定测试单元;历史版本引用的接口信息不受影响。需要编辑权限。
//	@Tags		测试单元
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint	true	"测试集 ID"
//	@Param		unitID	path	uint	true	"测试单元 ID"
//	@Success	200	{object}	okResp		"已删除"
//	@Failure	400	{object}	errorResp	"无效的 ID"
//	@Failure	403	{object}	errorResp	"无编辑权限"
//	@Failure	404	{object}	errorResp	"单元不存在"
//	@Router		/test-sets/{id}/units/{unitID} [delete]
func (s *Server) handleDeleteUnit(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	uid, _ := userIDOf(c)
	canEdit, err := service.CanEdit(s.DB, setID, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return
	}
	unitID, ok := parseID(c, "unitID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的单元 ID")
		return
	}
	var unit model.TestUnit
	if err := s.DB.Where("id = ? AND test_set_id = ?", unitID, setID).First(&unit).Error; err != nil {
		writeErr(c, http.StatusNotFound, "单元不存在")
		return
	}
	s.DB.Delete(&unit)
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}
