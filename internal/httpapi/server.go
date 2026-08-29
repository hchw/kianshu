package httpapi

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github/hchw/kianshu/internal/config"
	"github/hchw/kianshu/internal/crypto"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/openai"
	"github/hchw/kianshu/internal/scheduler"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"

	_ "github/hchw/kianshu/docs"
)

const userIDKey = "userID"

// Server holds dependencies shared by all handlers.
type Server struct {
	DB     *gorm.DB
	Cfg    *config.Config
	Cipher *crypto.AESCipher
	LLM    *openai.Client
	// Schedules drives cron-triggered flow runs behind the scheduler plugin
	// interface (default: gocron).
	Schedules *service.ScheduleManager
	// AgentLLM, when set, replaces the provider-bound chat client used by the
	// agent endpoints (test injection hook).
	AgentLLM service.ChatProvider
	// AgentBus carries in-memory events of active agent runs so clients that
	// reconnect (page refresh) can pick up missed events.
	AgentBus *service.AgentEventBus
	// RunBus broadcasts new execution logs to subscribers in real time.
	RunBus *service.RunEventBus
}

// New builds a Server and registers all routes.
func New(db *gorm.DB, cfg *config.Config) (*Server, error) {
	cipher, err := crypto.NewAESCipher(cfg.EncKey)
	if err != nil {
		return nil, err
	}
	backend, err := scheduler.NewGocron()
	if err != nil {
		return nil, err
	}
	log.Infof("调度器已初始化")
	runBus := service.NewRunEventBus()
	s := &Server{
		DB:        db,
		Cfg:       cfg,
		Cipher:    cipher,
		LLM:       openai.New(),
		Schedules: service.NewScheduleManager(db, backend, cfg.ExecTimeout, runBus),
		AgentBus:  service.NewAgentEventBus(),
		RunBus:    runBus,
	}
	// 单次 LLM 请求(连接+发送+流式读取)的界,防止长输出被过短的 HTTP 超时切断。
	s.LLM.HTTP.Timeout = cfg.LLMTimeout
	return s, nil
}

// Start begins background workers (scheduled runs).
func (s *Server) Start() {
	s.Schedules.Start()
}

// Stop shuts down background workers.
func (s *Server) Stop() {
	s.Schedules.Stop()
}

// Routes returns the gin engine with all routes registered.
func (s *Server) Routes() *gin.Engine {
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{Output: log.StandardLogger().Writer()}), gin.Recovery())

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("/api")
	api.POST("/auth/register", s.handleRegister)
	api.POST("/auth/login", s.handleLogin)

	auth := api.Group("")
	auth.Use(s.withAuth())
	{
		auth.POST("/auth/logout", s.handleLogout)
		auth.POST("/auth/refresh", s.handleRefreshToken)
		auth.GET("/dashboard", s.handleDashboard)

		auth.GET("/test-sets", s.handleListTestSets)
		auth.POST("/test-sets", s.handleCreateTestSet)
		auth.GET("/test-sets/:id", s.handleGetTestSet)
		auth.PATCH("/test-sets/:id", s.handleUpdateTestSet)
		auth.GET("/test-sets/:id/members", s.handleListMembers)
		auth.POST("/test-sets/:id/members", s.handleAddMember)
		auth.DELETE("/test-sets/:id/members/:userID", s.handleRemoveMember)
		auth.GET("/test-sets/:id/members/search", s.handleSearchUsers)
		auth.POST("/test-sets/:id/imports", s.handleImport)
		auth.GET("/test-sets/:id/units", s.handleListUnits)
		auth.GET("/test-sets/:id/units/tool-list", s.handleToolListUnits)
		auth.GET("/test-sets/:id/units/:unitID", s.handleGetUnit)
		auth.DELETE("/test-sets/:id/units/:unitID", s.handleDeleteUnit)

		auth.GET("/test-sets/:id/flows", s.handleListFlows)
		auth.POST("/test-sets/:id/flows", s.handleCreateFlow)
		auth.GET("/flow/flows/:flowID/draft", s.handleGetDraft)
		auth.DELETE("/flow/flows/:flowID", s.handleDeleteFlow)
		auth.POST("/flow/flows/:flowID/duplicate", s.handleDuplicateFlow)
		auth.PATCH("/flow/flows/:flowID", s.handleRenameFlow)
		auth.PUT("/flow/flows/:flowID/draft", s.handleUpdateDraft)
		auth.PATCH("/flow/flows/:flowID/thinking", s.handleUpdateFlowThinking)
		auth.POST("/flow/flows/:flowID/draft/validate", s.handleValidateDraft)
		auth.POST("/flow/flows/:flowID/draft/trial-run", s.handleTrialRun)
		auth.POST("/flow/flows/:flowID/versions", s.handleSaveEnable)
		auth.GET("/flow/flows/:flowID/versions", s.handleListVersions)
		auth.GET("/flow/flows/:flowID/versions/:versionNo", s.handleGetVersion)
		auth.POST("/flow/flows/:flowID/versions/:versionNo/run", s.handleRunVersion)
		auth.GET("/flow/flows/:flowID/runs", s.handleListRuns)
		auth.GET("/flow/flows/:flowID/runs/subscribe", s.handleSubscribeRuns)
		auth.GET("/flow/flows/:flowID/runs/:runID", s.handleGetRun)
		auth.GET("/flow/flows/:flowID/schedules", s.handleListSchedules)
		auth.POST("/flow/flows/:flowID/schedules", s.handleCreateSchedule)
		auth.PATCH("/flow/flows/:flowID/schedules/:scheduleID", s.handleUpdateSchedule)
		auth.PATCH("/flow/flows/:flowID/schedules/:scheduleID/enabled", s.handleSetScheduleEnabled)
		auth.DELETE("/flow/flows/:flowID/schedules/:scheduleID", s.handleDeleteSchedule)

		auth.POST("/flow/flows/:flowID/agent/submit", s.handleAgentSubmit)
		auth.GET("/flow/flows/:flowID/agent/subscribe", s.handleAgentSubscribe)
		auth.POST("/flow/flows/:flowID/agent/subscribe", s.handleAgentSubscribe)
		auth.GET("/flow/flows/:flowID/agent/session", s.handleAgentSession)
		auth.POST("/flow/flows/:flowID/agent/resume", s.handleAgentResume)
		auth.POST("/flow/flows/:flowID/agent/new", s.handleAgentNew)
		auth.POST("/flow/flows/:flowID/agent/compress", s.handleAgentCompress)

		auth.GET("/providers", s.handleListProviders)
		auth.POST("/providers", s.handleCreateProvider)
		auth.GET("/providers/:id", s.handleGetProvider)
		auth.PATCH("/providers/:id", s.handleUpdateProvider)
		auth.DELETE("/providers/:id", s.handleDeleteProvider)
		auth.POST("/providers/:id/test", s.handleTestProvider)

		auth.POST("/utils/cron/describe", s.handleDescribeCron)
		auth.POST("/utils/sign", s.signAuth(), s.handleSign)
	}

	// 发布包使用 web-dist，开发环境使用 web/dist。
	frontendDir := "web-dist"
	if _, err := os.Stat(frontendDir); os.IsNotExist(err) {
		frontendDir = "web/dist"
	}
	if _, err := os.Stat(frontendDir); err == nil {
		// 不能将 StaticFS 注册到根路径，否则会与 /api 的路由冲突。
		// Vite 生成的静态资源默认位于 assets 目录；其余前端路由交给 SPA fallback。
		r.StaticFS("/assets", gin.Dir(frontendDir+"/assets", false))
		// public 目录下的文件会被 Vite 原样复制到 dist 根目录。
		for _, name := range []string{"kianshu.png", "main.png", "icons.svg", "favicon.svg"} {
			r.StaticFile("/"+name, frontendDir+"/"+name)
		}
		r.GET("/", func(c *gin.Context) {
			c.File(frontendDir + "/index.html")
		})
		r.NoRoute(func(c *gin.Context) {
			if c.Request.Method == http.MethodGet {
				c.File(frontendDir + "/index.html")
				return
			}
			c.Status(http.StatusNotFound)
		})
	}

	return r
}

// withAuth resolves the session and injects the current user ID.
func (s *Server) withAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := s.authenticate(c)
		if err != nil {
			writeErr(c, http.StatusUnauthorized, "未登录或会话已失效")
			c.Abort()
			return
		}
		c.Set(userIDKey, userID)
		c.Next()
	}
}

func (s *Server) authenticate(c *gin.Context) (uint, error) {
	h := c.GetHeader("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	if token == "" || token == h {
		return 0, errors.New("missing token")
	}
	var sess model.Session
	if err := s.DB.Where("token_hash = ?", hashToken(token)).First(&sess).Error; err != nil {
		return 0, err
	}
	if sess.ExpiresAt.Before(now()) {
		return 0, errors.New("session expired")
	}
	return sess.UserID, nil
}

func userIDOf(c *gin.Context) (uint, bool) {
	v, ok := c.Get(userIDKey)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

func writeJSON(c *gin.Context, status int, v any) {
	c.JSON(status, v)
}

func writeErr(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func parseID(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}
