package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"github/hchw/kianshu/internal/model"
)

// NeedConfirmationError carries the non-standard issues detected during swagger
// parsing that require user confirmation before import proceeds.
type NeedConfirmationError struct {
	Issues []string
}

func (e *NeedConfirmationError) Error() string {
	return strings.Join(e.Issues, "; ")
}

func (e *NeedConfirmationError) Unwrap() error { return ErrNeedConfirmation }

// ImportResult reports the outcome of a swagger import.
type ImportResult struct {
	ImportID uint
	Created  int
	Updated  int
}

// ImportSwagger parses a swagger document and upserts test units in the given
// test set, keyed by normalized method+path. When the document is non-standard
// (ParseSwagger returns issues) and confirm is false, it returns
// NeedConfirmationError so the caller can ask the user to confirm.
func ImportSwagger(db *gorm.DB, testSetID uint, source string, data []byte, confirm bool) (*ImportResult, error) {
	doc, issues, err := ParseSwagger(data)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 && !confirm {
		return nil, &NeedConfirmationError{Issues: issues}
	}

	imp := model.Import{
		TestSetID:  testSetID,
		Source:     source,
		RawSwagger: string(data),
	}
	if err := db.Create(&imp).Error; err != nil {
		return nil, err
	}

	res := &ImportResult{ImportID: imp.ID}
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, op := range doc.Operations {
			unit := model.TestUnit{
				TestSetID:   testSetID,
				Method:      op.Method,
				Path:        op.Path,
				Slug:        Slug(op.Method, op.Path),
				Tag:         firstNonEmpty(op.Tag, "未分类"),
				Name:        op.Name,
				Params:      op.Params,
				RequestBody: op.RequestBody,
				Responses:   op.Responses,
				Security:    op.Security,
				Spec:        op.Spec,
			}
			var existing model.TestUnit
			err := tx.Unscoped().
				Where("test_set_id = ? AND method = ? AND path = ?", testSetID, op.Method, op.Path).
				First(&existing).Error
			if err == nil {
				unit.ID = existing.ID
				unit.CreatedAt = existing.CreatedAt
				unit.DeletedAt = gorm.DeletedAt{}
				unit.ImportID = imp.ID
				if err := tx.Unscoped().Model(&existing).Select("*").Omit("id", "created_at").Updates(unit).Error; err != nil {
					return err
				}
				res.Updated++
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&unit).Error; err != nil {
					return err
				}
				res.Created++
			} else {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
