package service

import (
	"errors"
	"strings"

	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

// ErrBackgroundDocNotFound indicates a missing background document.
var ErrBackgroundDocNotFound = errors.New("背景文档不存在")

// CreateBackgroundDocument creates a document in a test set.
func CreateBackgroundDocument(db *gorm.DB, testSetID, userID uint, name, content string) (*model.BackgroundDocument, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(content) == "" {
		return nil, errors.New("名称和内容不能为空")
	}
	doc := &model.BackgroundDocument{TestSetID: testSetID, Name: name, Content: content, CreatedBy: userID}
	if err := db.Create(doc).Error; err != nil {
		return nil, err
	}
	return doc, nil
}

// ListBackgroundDocuments lists a test set's documents.
func ListBackgroundDocuments(db *gorm.DB, testSetID uint) ([]model.BackgroundDocument, error) {
	var out []model.BackgroundDocument
	if err := db.Where("test_set_id = ?", testSetID).Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetBackgroundDocument returns one document.
func GetBackgroundDocument(db *gorm.DB, testSetID, id uint) (*model.BackgroundDocument, error) {
	var doc model.BackgroundDocument
	if err := db.Where("id = ? AND test_set_id = ?", id, testSetID).First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBackgroundDocNotFound
		}
		return nil, err
	}
	return &doc, nil
}

// UpdateBackgroundDocument edits a document without mutating dependent trees.
func UpdateBackgroundDocument(db *gorm.DB, testSetID, id uint, name, content *string) (*model.BackgroundDocument, error) {
	doc, err := GetBackgroundDocument(db, testSetID, id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, errors.New("名称不能为空")
		}
		doc.Name = n
	}
	if content != nil {
		if strings.TrimSpace(*content) == "" {
			return nil, errors.New("内容不能为空")
		}
		doc.Content = *content
	}
	if err := db.Save(doc).Error; err != nil {
		return nil, err
	}
	return doc, nil
}

// DeleteBackgroundDocument soft-deletes a document. Referenced content remains
// readable through historical snapshots.
func DeleteBackgroundDocument(db *gorm.DB, testSetID, id uint) error {
	return db.Where("id = ? AND test_set_id = ?", id, testSetID).Delete(&model.BackgroundDocument{}).Error
}
