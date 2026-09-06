package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"

	"github/hchw/kianshu/internal/caseflow"
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

// xmindTopic mirrors the XMind content.json topic shape.
type xmindTopic struct {
	ID       string         `json:"id"`
	Class    string         `json:"class"`
	Title    string         `json:"title"`
	Children *xmindAttached `json:"children,omitempty"`
}

type xmindAttached struct {
	Attached []*xmindTopic `json:"attached"`
}

type xmindSheet struct {
	ID        string      `json:"id"`
	Class     string      `json:"class"`
	Title     string      `json:"title"`
	RootTopic *xmindTopic `json:"rootTopic"`
}

func toXMindTopic(n *caseflow.Node) *xmindTopic {
	if n == nil {
		return nil
	}
	t := &xmindTopic{ID: n.ID, Class: "topic", Title: n.Title}
	if len(n.Children) > 0 {
		attached := &xmindAttached{}
		for _, child := range n.Children {
			attached.Attached = append(attached.Attached, toXMindTopic(child))
		}
		t.Children = attached
	}
	return t
}

// ExportCaseFlowXMind 导出当前草稿或指定版本的 XMind 文件，返回字节与文件名。
func ExportCaseFlowXMind(db *gorm.DB, caseFlowID uint, versionNo *int) ([]byte, string, error) {
	var cf model.CaseFlow
	if err := db.First(&cf, caseFlowID).Error; err != nil {
		return nil, "", ErrCaseFlowNotFound
	}
	title := cf.Name
	treeJSON := ""
	if versionNo == nil {
		d, err := getCaseFlowDraft(db, caseFlowID)
		if err != nil {
			return nil, "", err
		}
		treeJSON = d.Tree
	} else {
		var v model.CaseFlowVersion
		if err := db.Where("case_flow_id = ? AND version_no = ?", caseFlowID, *versionNo).First(&v).Error; err != nil {
			return nil, "", ErrCaseFlowNotFound
		}
		treeJSON = v.Tree
	}
	tree, err := parseCaseTree(treeJSON)
	if err != nil {
		return nil, "", err
	}
	sheet := xmindSheet{ID: fmt.Sprintf("sheet-%d", caseFlowID), Class: "sheet", Title: title, RootTopic: toXMindTopic(tree.Root)}
	content, err := json.Marshal([]xmindSheet{sheet})
	if err != nil {
		return nil, "", err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("content.json")
	if err != nil {
		return nil, "", err
	}
	if _, err := f.Write(content); err != nil {
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), title + ".xmind", nil
}
