package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"
	"github/hchw/kianshu/internal/service"

	"github.com/gin-gonic/gin"
)

// 背景文档

func (s *Server) requireEditAccess(c *gin.Context) (uint, uint, bool) {
	uid, _ := userIDOf(c)
	id, ok := parseID(c, "id")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的测试集 ID")
		return 0, 0, false
	}
	canEdit, err := service.CanEdit(s.DB, id, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, 0, false
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return 0, 0, false
	}
	return id, uid, true
}

func (s *Server) handleListBackgroundDocs(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	docs, err := service.ListBackgroundDocuments(s.DB, setID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"documents": docs})
}

func (s *Server) handleCreateBackgroundDoc(c *gin.Context) {
	setID, uid, ok := s.requireEditAccess(c)
	if !ok {
		return
	}
	var req struct{ Name, Content string }
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	doc, err := service.CreateBackgroundDocument(s.DB, setID, uid, req.Name, req.Content)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusCreated, doc)
}

func (s *Server) handleGetBackgroundDoc(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	docID, ok := parseID(c, "docID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的文档 ID")
		return
	}
	doc, err := service.GetBackgroundDocument(s.DB, setID, docID)
	if err != nil {
		writeErr(c, http.StatusNotFound, "文档不存在")
		return
	}
	writeJSON(c, http.StatusOK, doc)
}

func (s *Server) handleUpdateBackgroundDoc(c *gin.Context) {
	setID, _, ok := s.requireEditAccess(c)
	if !ok {
		return
	}
	docID, ok := parseID(c, "docID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的文档 ID")
		return
	}
	var req struct{ Name, Content *string }
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	doc, err := service.UpdateBackgroundDocument(s.DB, setID, docID, req.Name, req.Content)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, doc)
}

func (s *Server) handleDeleteBackgroundDoc(c *gin.Context) {
	setID, _, ok := s.requireEditAccess(c)
	if !ok {
		return
	}
	docID, ok := parseID(c, "docID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的文档 ID")
		return
	}
	if err := service.DeleteBackgroundDocument(s.DB, setID, docID); err != nil {
		writeErr(c, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"deleted": true})
}

// Case Flow 权限辅助

func (s *Server) caseFlowTestSet(caseFlowID uint) (uint, bool) {
	var f struct{ TestSetID uint }
	if err := s.DB.Table("case_flows").Select("test_set_id").Where("id = ?", caseFlowID).Scan(&f).Error; err != nil {
		return 0, false
	}
	return f.TestSetID, f.TestSetID != 0
}

func (s *Server) caseFlowReadable(c *gin.Context) (uint, uint, bool) {
	uid, _ := userIDOf(c)
	cfID, ok := parseID(c, "caseFlowID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的用例流 ID")
		return 0, 0, false
	}
	ts, ok := s.caseFlowTestSet(cfID)
	if !ok {
		writeErr(c, http.StatusNotFound, "用例流不存在")
		return 0, 0, false
	}
	canRead, err := service.CanRead(s.DB, ts, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, 0, false
	}
	if !canRead {
		writeErr(c, http.StatusForbidden, "无权访问该用例流")
		return 0, 0, false
	}
	return cfID, uid, true
}

func (s *Server) caseFlowEditable(c *gin.Context) (uint, uint, bool) {
	cfID, uid, ok := s.caseFlowReadable(c)
	if !ok {
		return 0, 0, false
	}
	ts, _ := s.caseFlowTestSet(cfID)
	canEdit, err := service.CanEdit(s.DB, ts, uid)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "权限检查失败")
		return 0, 0, false
	}
	if !canEdit {
		writeErr(c, http.StatusForbidden, "无编辑权限")
		return 0, 0, false
	}
	return cfID, uid, true
}

// Case Flow CRUD

func (s *Server) handleListCaseFlows(c *gin.Context) {
	setID, ok := s.requireReadAccess(c)
	if !ok {
		return
	}
	flows, err := service.ListCaseFlows(s.DB, setID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"case_flows": flows})
}

func (s *Server) handleCreateCaseFlow(c *gin.Context) {
	setID, uid, ok := s.requireEditAccess(c)
	if !ok {
		return
	}
	var req struct {
		Name    string                `json:"name"`
		Sources []service.SourceInput `json:"sources"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	// 前端默认以 all 作为来源；测试集没有背景文档和接口测试单元时，
	// 创建出的用例流没有可生成的内容。
	for _, source := range req.Sources {
		if source.Kind != "all" {
			continue
		}
		var documents, units int64
		if err := s.DB.Model(&model.BackgroundDocument{}).Where("test_set_id = ?", setID).Count(&documents).Error; err != nil {
			writeErr(c, http.StatusInternalServerError, "查询测试集资源失败")
			return
		}
		if err := s.DB.Model(&model.TestUnit{}).Where("test_set_id = ?", setID).Count(&units).Error; err != nil {
			writeErr(c, http.StatusInternalServerError, "查询测试集资源失败")
			return
		}
		if documents == 0 && units == 0 {
			writeErr(c, http.StatusBadRequest, "测试集至少需要一个背景文档或测试单元")
			return
		}
		break
	}
	cf, err := service.CreateCaseFlow(s.DB, setID, uid, req.Name, req.Sources)
	if err != nil {
		if errors.Is(err, service.ErrCaseSourceRequired) {
			writeErr(c, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(c, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(c, http.StatusCreated, cf)
}

func (s *Server) handleGetCaseFlow(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	cf, err := service.GetCaseFlow(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusNotFound, "用例流不存在")
		return
	}
	writeJSON(c, http.StatusOK, cf)
}

func (s *Server) handleRenameCaseFlow(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		writeErr(c, http.StatusBadRequest, "名称不能为空")
		return
	}
	cf, err := service.RenameCaseFlow(s.DB, cfID, body.Name)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "重命名失败")
		return
	}
	writeJSON(c, http.StatusOK, cf)
}

func (s *Server) handleDeleteCaseFlow(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	if err := service.DeleteCaseFlow(s.DB, cfID); err != nil {
		writeErr(c, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"deleted": true})
}

func (s *Server) handleGetCaseFlowDraft(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	view, err := service.GetCaseTreeView(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusNotFound, "草稿不存在")
		return
	}
	writeJSON(c, http.StatusOK, view)
}

func (s *Server) handleUpdateCaseFlowDraft(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var req struct {
		Revision uint   `json:"revision"`
		Tree     string `json:"tree"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := service.UpdateCaseFlowDraft(s.DB, cfID, req.Revision, req.Tree)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleSaveCaseFlowVersion(c *gin.Context) {
	cfID, uid, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	v, err := service.SaveCaseFlowVersion(s.DB, cfID, uid)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusCreated, v)
}

func (s *Server) handleListCaseFlowVersions(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	versions, err := service.ListCaseFlowVersions(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"versions": versions})
}

func (s *Server) handleGetCaseFlowVersion(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	versionNo, ok := parseID(c, "versionNo")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的版本号")
		return
	}
	versions, err := service.ListCaseFlowVersions(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	for _, v := range versions {
		if v.VersionNo == int(versionNo) {
			writeJSON(c, http.StatusOK, v)
			return
		}
	}
	writeErr(c, http.StatusNotFound, "版本不存在")
}

func (s *Server) handleRestoreCaseFlowVersion(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	versionNo, ok := parseID(c, "versionNo")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的版本号")
		return
	}
	d, err := service.RestoreCaseFlowVersion(s.DB, cfID, int(versionNo))
	if err != nil {
		writeErr(c, http.StatusNotFound, "版本不存在")
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleListCaseSources(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	sources, err := service.ListCaseSources(s.DB, cfID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"sources": sources})
}

func (s *Server) handleAddCaseSource(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var req service.SourceInput
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	if err := service.AddCaseSource(s.DB, cfID, req); err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	sources, _ := service.ListCaseSources(s.DB, cfID)
	writeJSON(c, http.StatusCreated, gin.H{"sources": sources})
}

func (s *Server) handleRemoveCaseSource(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	sourceID, ok := parseID(c, "sourceID")
	if !ok {
		writeErr(c, http.StatusBadRequest, "无效的来源 ID")
		return
	}
	if err := service.RemoveCaseSource(s.DB, cfID, sourceID); err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"deleted": true})
}

// 用例节点操作

func revisionOf(c *gin.Context) (uint, bool) {
	var req struct {
		Revision uint `json:"revision"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return 0, false
	}
	return req.Revision, true
}

func (s *Server) handleAddCaseNode(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	var req struct {
		Revision uint   `json:"revision"`
		ParentID string `json:"parent_id"`
		Title    string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := service.AddCaseNode(s.DB, cfID, req.Revision, req.ParentID, req.Title)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusCreated, d)
}

func (s *Server) handleUpdateCaseNode(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	nodeID := c.Param("nodeID")
	var req struct {
		Revision     uint     `json:"revision"`
		Title        *string  `json:"title"`
		Description  *string  `json:"description"`
		Precondition *string  `json:"precondition"`
		Input        *string  `json:"input"`
		Expected     *string  `json:"expected"`
		X            *float64 `json:"x"`
		Y            *float64 `json:"y"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := service.UpdateCaseNode(s.DB, cfID, req.Revision, nodeID, service.CaseNodeUpdate{
		Title: req.Title, Description: req.Description, Precondition: req.Precondition,
		Input: req.Input, Expected: req.Expected, X: req.X, Y: req.Y,
	})
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleMoveCaseNode(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	nodeID := c.Param("nodeID")
	var req struct {
		Revision    uint   `json:"revision"`
		NewParentID string `json:"new_parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := service.MoveCaseNode(s.DB, cfID, req.Revision, nodeID, req.NewParentID)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleDeleteCaseNode(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	nodeID := c.Param("nodeID")
	rev, ok := revisionOf(c)
	if !ok {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := service.DeleteCaseNode(s.DB, cfID, rev, nodeID)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleSetCaseStatus(c *gin.Context) {
	cfID, _, ok := s.caseFlowEditable(c)
	if !ok {
		return
	}
	nodeID := c.Param("nodeID")
	var req struct {
		Revision uint   `json:"revision"`
		Status   string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, http.StatusBadRequest, "请求体不合法")
		return
	}
	if req.Status != caseflow.StatusCovered && req.Status != caseflow.StatusUncovered {
		writeErr(c, http.StatusBadRequest, "无效的用例状态")
		return
	}
	d, err := service.SetCaseStatus(s.DB, cfID, req.Revision, nodeID, req.Status)
	if err != nil {
		writeErr(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, d)
}

func (s *Server) handleListNodeFlows(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	nodeID := c.Param("nodeID")
	versions, err := service.ListCaseNodeFlows(s.DB, cfID, nodeID)
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"flows": versions})
}

func parseIDInt(c *gin.Context, name string) (int, bool) {
	n, err := strconv.Atoi(c.Param(name))
	if err != nil {
		return 0, false
	}
	return n, true
}

// handleExportCaseFlow 导出当前草稿或指定版本的 XMind 文件。
func (s *Server) handleExportCaseFlow(c *gin.Context) {
	cfID, _, ok := s.caseFlowReadable(c)
	if !ok {
		return
	}
	var versionNo *int
	if v := c.Query("version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "无效的版本号")
			return
		}
		versionNo = &n
	}
	data, filename, err := service.ExportCaseFlowXMind(s.DB, cfID, versionNo)
	if err != nil {
		writeErr(c, http.StatusNotFound, "导出失败")
		return
	}
	// 同时提供 ASCII 回退文件名和 RFC 5987 UTF-8 文件名，避免中文文件名在浏览器中乱码。
	encodedFilename := url.PathEscape(filename)
	fallback := fmt.Sprintf("case-flow-%d.xmind", cfID)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, encodedFilename))
	c.Data(http.StatusOK, "application/vnd.xmind.workbook", data)
}
