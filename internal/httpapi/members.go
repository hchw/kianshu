package httpapi

import (
	"net/http"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

type addMemberReq struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
}

// handleAddMember adds or updates a member role on a test set.
//
//	@Summary	添加/更新成员
//	@Description	为测试集添加成员并指定角色(read|edit)。已存在则更新角色。仅 owner 可管理成员。不能把自己加为成员。
//	@Tags		测试集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		uint				true	"测试集 ID"
//	@Param		body	body		addMemberReq			true	"成员信息(user_id 与角色 read/edit)"
//	@Success	201		{object}	model.TestSetMember	"成员记录"
//	@Failure	400		{object}	errorResp			"无效的 ID / 角色不合法 / 目标用户不存在"
//	@Failure	403		{object}	errorResp			"仅 owner 可管理成员"
//	@Failure	500		{object}	errorResp			"服务端错误"
//	@Router		/test-sets/{id}/members [post]
func (s *Server) handleAddMember(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	role, err := service.AccessRole(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if role != service.RoleOwner {
		writeErr(c, http.StatusForbidden, "仅 owner 可管理成员")
		return
	}
	var req addMemberReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	if req.Role != model.RoleRead && req.Role != model.RoleEdit {
		writeErr(c, http.StatusBadRequest, "角色必须是 read 或 edit")
		return
	}
	if req.UserID == uid {
		writeErr(c, http.StatusBadRequest, "不能把自己加为成员")
		return
	}
	var target model.User
	if err := s.DB.First(&target, req.UserID).Error; err != nil {
		writeErr(c, http.StatusBadRequest, "目标用户不存在")
		return
	}
	m := model.TestSetMember{TestSetID: id, UserID: req.UserID, Role: req.Role}
	var existing model.TestSetMember
	err = s.DB.Where("test_set_id = ? AND user_id = ?", id, req.UserID).First(&existing).Error
	if err == nil {
		if err := s.DB.Model(&existing).Update("role", req.Role).Error; err != nil {
			writeErr(c, http.StatusInternalServerError, "更新失败")
			return
		}
		m = existing
	} else {
		if err := s.DB.Create(&m).Error; err != nil {
			writeErr(c, http.StatusInternalServerError, "添加失败")
			return
		}
	}
	writeJSON(c, http.StatusCreated, m)
}

// handleRemoveMember removes a member from a test set.
//
//	@Summary	移除成员
//	@Description	将指定用户从测试集成员中移除。仅 owner 可管理成员。
//	@Tags		测试集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	uint	true	"测试集 ID"
//	@Param		userID	path	uint	true	"要移除的用户 ID"
//	@Success	200		{object}	okResp	"已移除"
//	@Failure	400		{object}	errorResp	"无效的 ID"
//	@Failure	403		{object}	errorResp	"仅 owner 可管理成员"
//	@Router		/test-sets/{id}/members/{userID} [delete]
func (s *Server) handleRemoveMember(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	role, err := service.AccessRole(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return
	}
	if role != service.RoleOwner {
		writeErr(c, http.StatusForbidden, "仅 owner 可管理成员")
		return
	}
	memberID, ok := parseID(c, "userID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的成员 ID")
		return
	}
	s.DB.Where("test_set_id = ? AND user_id = ?", id, memberID).Delete(&model.TestSetMember{})
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}
