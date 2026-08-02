package service

import (
	"github/hchw/kianshu/internal/model"

	"gorm.io/gorm"
)

// RoleResult describes a user's access to a test set.
type RoleResult int

const (
	// RoleNone means the user is neither owner nor member.
	RoleNone RoleResult = iota
	// RoleRead allows reading only.
	RoleRead
	// RoleEdit allows modifying flows, imports, and settings.
	RoleEdit
	// RoleOwner is the test set owner with full control.
	RoleOwner
)

// AccessRole returns the user's access role for a test set.
func AccessRole(db *gorm.DB, testSetID, userID uint) (RoleResult, error) {
	var ts model.TestSet
	if err := db.First(&ts, testSetID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RoleNone, nil
		}
		return RoleNone, err
	}
	if ts.OwnerID == userID {
		return RoleOwner, nil
	}
	var m model.TestSetMember
	err := db.Where("test_set_id = ? AND user_id = ?", testSetID, userID).First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return RoleNone, nil
	}
	if err != nil {
		return RoleNone, err
	}
	if m.Role == "edit" {
		return RoleEdit, nil
	}
	return RoleRead, nil
}

// CanRead reports whether the user may read the test set.
func CanRead(db *gorm.DB, testSetID, userID uint) (bool, error) {
	r, err := AccessRole(db, testSetID, userID)
	return r != RoleNone, err
}

// CanEdit reports whether the user may modify the test set.
func CanEdit(db *gorm.DB, testSetID, userID uint) (bool, error) {
	r, err := AccessRole(db, testSetID, userID)
	return r == RoleEdit || r == RoleOwner, err
}
