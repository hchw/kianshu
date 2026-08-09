package httpapi

import (
	"net/http"
	"strings"

	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

type addMemberReq struct {
	UserID uint   `json:"user_id" validate:"required" example:"2"`
	Role   string `json:"role" validate:"required" enum:"read,edit" example:"edit"`
}

// memberView 是成员列表中的一条记录,包含用户名方便前端展示。
type memberView struct {
	UserID   uint   `json:"user_id" example:"2"`
	Username string `json:"username" example:"zhangsan"`
	Role     string `json:"role" example:"edit"`
}

// userBrief 是用户搜索结果的精简视图。
type userBrief struct {
	ID       uint   `json:"id" example:"2"`
	Username string `json:"username" example:"zhangsan"`
}

// handleListMembers 返回测试集的所有者与成员列表(含用户名)。
//
//	@Summary	成员列表
//	@Description	获取测试集的所有者信息及所有成员(含用户名)。owner/edit/read 均可查看。
//	@Tags		测试集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		uint	true	"测试集 ID"
//	@Success	200	{object}	memberListResp	"成员列表"
//	@Failure	400	{object}	errorResp	"无效的 ID"
//	@Failure	403	{object}	errorResp	"无权限"
//	@Router		/test-sets/{id}/members [get]
func (s *Server) handleListMembers(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	_, err := service.AccessRole(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusForbidden, "无权限访问该测试集")
		return
	}

	// 查询所有者
	var ts model.TestSet
	if err := s.DB.First(&ts, id).Error; err != nil {
		writeErr(c, http.StatusNotFound, "测试集不存在")
		return
	}
	var owner model.User
	s.DB.First(&owner, ts.OwnerID)

	// 查询成员并关联用户名
	type row struct {
		UserID   uint
		Username string
		Role     string
	}
	var rows []row
	s.DB.Table("test_set_members").
		Select("test_set_members.user_id, users.username, test_set_members.role").
		Joins("JOIN users ON users.id = test_set_members.user_id").
		Where("test_set_members.test_set_id = ?", id).
		Scan(&rows)

	members := make([]memberView, 0, len(rows))
	for _, r := range rows {
		members = append(members, memberView{UserID: r.UserID, Username: r.Username, Role: r.Role})
	}

	writeJSON(c, http.StatusOK, gin.H{
		"owner":  memberView{UserID: owner.ID, Username: owner.Username, Role: "owner"},
		"members": members,
	})
}

// handleSearchUsers 按用户名搜索用户,用于添加成员时的自动补全。
//
//	@Summary	搜索用户
//	@Description	根据用户名前缀搜索用户,排除当前用户及已是成员的用户,最多返回 10 条。
//	@Tags		测试集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		uint	true	"测试集 ID"
//	@Param		q	query		string	true	"搜索关键词(用户名前缀)"
//	@Success	200	{object}	userSearchResp	"搜索结果"
//	@Failure	400	{object}	errorResp	"无效的 ID"
//	@Failure	403	{object}	errorResp	"无权限"
//	@Router		/test-sets/{id}/members/search [get]
func (s *Server) handleSearchUsers(c *gin.Context) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return
	}
	role, err := service.AccessRole(s.DB, id, uid)
	if err != nil || role != service.RoleOwner {
		writeErr(c, http.StatusForbidden, "仅 owner 可管理成员")
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		writeJSON(c, http.StatusOK, gin.H{"users": []userBrief{}})
		return
	}

	// 查询已是成员的用户 ID
	var memberIDs []uint
	s.DB.Model(&model.TestSetMember{}).
		Where("test_set_id = ?", id).
		Pluck("user_id", &memberIDs)

	// 排除当前用户、owner、已有成员
	exclude := append(memberIDs, uid)
	var ts model.TestSet
	if err := s.DB.First(&ts, id).Error; err == nil {
		exclude = append(exclude, ts.OwnerID)
	}

	var users []model.User
	s.DB.Where("username LIKE ?", q+"%").
		Where("id NOT IN ?", exclude).
		Limit(10).
		Find(&users)

	result := make([]userBrief, 0, len(users))
	for _, u := range users {
		result = append(result, userBrief{ID: u.ID, Username: u.Username})
	}
	writeJSON(c, http.StatusOK, gin.H{"users": result})
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
	writeJSON(c, http.StatusCreated, memberView{UserID: m.UserID, Username: target.Username, Role: m.Role})
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
