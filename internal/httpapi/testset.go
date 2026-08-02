package httpapi

import (
	"net/http"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

// handleListTestSets returns the test sets the caller owns or is a member of.
//
//	@Summary	列出测试集
//	@Description	返回当前用户拥有或作为成员参与的所有测试集。
//	@Tags		测试集
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	testSetListResp	"测试集列表"
//	@Router		/test-sets [get]
func (s *Server) handleListTestSets(c *gin.Context) {
	uid, _ := userIDOf(c)
	var owned []model.TestSet
	s.DB.Where("owner_id = ?", uid).Order("id").Find(&owned)
	var memberRows []model.TestSetMember
	s.DB.Where("user_id = ?", uid).Find(&memberRows)
	seen := map[uint]bool{}
	var sets []model.TestSet
	for _, o := range owned {
		seen[o.ID] = true
		sets = append(sets, o)
	}
	for _, m := range memberRows {
		if seen[m.TestSetID] {
			continue
		}
		var ts model.TestSet
		if err := s.DB.First(&ts, m.TestSetID).Error; err == nil {
			sets = append(sets, ts)
		}
	}
	writeJSON(c, http.StatusOK, gin.H{"test_sets": sets})
}

type createTestSetReq struct {
	Name string `json:"name" validate:"required" minLength:"1" example:"我的测试集"`
	Host string `json:"host" example:"https://api.example.com"`
}

// handleCreateTestSet creates a test set owned by the caller.
//
//	@Summary	创建测试集
//	@Description	创建一个新测试集,创建者即为 owner(完全权限)。host 为单一环境地址,可选。
//	@Tags		测试集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		createTestSetReq	true	"测试集信息"
//	@Success	201		{object}	model.TestSet		"创建成功"
//	@Failure	400		{object}	errorResp			"名称不能为空"
//	@Failure	500		{object}	errorResp			"创建失败"
//	@Router		/test-sets [post]
func (s *Server) handleCreateTestSet(c *gin.Context) {
	uid, _ := userIDOf(c)
	var req createTestSetReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		writeErr(c, http.StatusBadRequest, "名称不能为空")
		return
	}
	ts := model.TestSet{Name: req.Name, Host: req.Host, OwnerID: uid}
	if err := s.DB.Create(&ts).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(c, http.StatusCreated, ts)
}

// handleGetTestSet returns a single test set if the caller can read it.
//
//	@Summary	获取测试集详情
//	@Description	需要对该测试集具备读权限(owner / read / edit)。
//	@Tags		测试集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		uint			true	"测试集 ID"
//	@Success	200		{object}	model.TestSet	"测试集详情"
//	@Failure	400		{object}	errorResp		"无效的测试集 ID"
//	@Failure	403		{object}	errorResp		"无权访问该测试集"
//	@Failure	404		{object}	errorResp		"测试集不存在"
//	@Router		/test-sets/{id} [get]
func (s *Server) handleGetTestSet(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	canRead, err := service.CanRead(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if !canRead {
		writeErr(c, http.StatusForbidden, "无权访问该测试集")
		return
	}
	var ts model.TestSet
	if err := s.DB.First(&ts, id).Error; err != nil {
		writeErr(c, http.StatusNotFound, "测试集不存在")
		return
	}
	writeJSON(c, http.StatusOK, ts)
}

type updateTestSetReq struct {
	Name *string `json:"name" example:"新名称"`
	Host *string `json:"host" example:"https://new-api.example.com"`
}

// handleUpdateTestSet updates a test set the caller may edit.
//
//	@Summary	更新测试集
//	@Description	更新名称或 host,字段可省略(仅更新传入字段)。需要 edit 或 owner 权限。
//	@Tags		测试集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		uint				true	"测试集 ID"
//	@Param		body	body		updateTestSetReq	true	"更新内容"
//	@Success	200		{object}	model.TestSet		"更新后的测试集"
//	@Failure	400		{object}	errorResp			"无效的 ID / 请求体不合法 / 无更新内容"
//	@Failure	403		{object}	errorResp			"无编辑权限"
//	@Failure	404		{object}	errorResp			"测试集不存在"
//	@Router		/test-sets/{id} [patch]
func (s *Server) handleUpdateTestSet(c *gin.Context) {
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
	var req updateTestSetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	var ts model.TestSet
	if err := s.DB.First(&ts, id).Error; err != nil {
		writeErr(c, http.StatusNotFound, "测试集不存在")
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Host != nil {
		updates["host"] = *req.Host
	}
	if len(updates) == 0 {
		writeErr(c, http.StatusBadRequest, "无更新内容")
		return
	}
	if err := s.DB.Model(&ts).Updates(updates).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(c, http.StatusOK, ts)
}
