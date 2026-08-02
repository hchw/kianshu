package httpapi

import (
	"net/http"
	"strings"

	"github/hchw/kianshu/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type RegisterReq struct {
	Username string `json:"username" validate:"required" minLength:"3" example:"testuser"`
	Password string `json:"password" validate:"required" minLength:"6" example:"123456"`
}

// handleRegister creates a new platform account.
//
//	@Summary	注册账号
//	@Description	使用用户名与密码注册新账号。用户名至少 3 字符,密码至少 6 字符,用户名全局唯一。
//	@Tags		认证
//	@Accept		json
//	@Produce	json
//	@Param		body	body		RegisterReq	true	"注册信息"
//	@Success	201		{object}	registerResp	"注册成功"
//	@Failure	400		{object}	errorResp	"参数不合法 / 用户名已存在"
//	@Failure	500		{object}	errorResp	"服务端错误"
//	@Router		/auth/register [post]
func (s *Server) handleRegister(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Password) < 6 {
		writeErr(c, http.StatusBadRequest, "用户名至少 3 字符,密码至少 6 字符")
		return
	}
	var count int64
	s.DB.Model(&model.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		writeErr(c, http.StatusBadRequest, "用户名已存在")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "注册失败")
		return
	}
	u := model.User{Username: req.Username, PasswordHash: string(hash)}
	if err := s.DB.Create(&u).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "注册失败")
		return
	}
	writeJSON(c, http.StatusCreated, gin.H{"id": u.ID, "username": u.Username})
}

// handleLogin authenticates a user and issues a bearer session token.
//
//	@Summary	用户登录
//	@Description	校验用户名与密码,成功后签发会话令牌。令牌需在后续请求中通过 `Authorization: Bearer <token>` 携带。
//	@Tags		认证
//	@Accept		json
//	@Produce	json
//	@Param		body	body		RegisterReq	true	"登录凭据"
//	@Success	200		{object}	loginResp	"登录成功,返回 token"
//	@Failure	400		{object}	errorResp	"请求体不合法"
//	@Failure	401		{object}	errorResp	"用户名或密码错误"
//	@Router		/auth/login [post]
func (s *Server) handleLogin(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	var u model.User
	if err := s.DB.Where("username = ?", req.Username).First(&u).Error; err != nil {
		writeErr(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		writeErr(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := newToken()
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "签发会话失败")
		return
	}
	sess := model.Session{
		TokenHash: hashToken(token),
		UserID:    u.ID,
		ExpiresAt: now().Add(s.Cfg.SessionTTL),
	}
	if err := s.DB.Create(&sess).Error; err != nil {
		writeErr(c, http.StatusInternalServerError, "签发会话失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"token": token, "id": u.ID, "username": u.Username})
}

// handleLogout revokes the current session token.
//
//	@Summary	退出登录
//	@Description	使当前会话令牌立即失效。
//	@Tags		认证
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	okResp	"已退出"
//	@Router		/auth/logout [post]
func (s *Server) handleLogout(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token != "" {
		s.DB.Where("token_hash = ?", hashToken(token)).Delete(&model.Session{})
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true})
}
