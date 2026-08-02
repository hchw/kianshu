package model

import (
	"time"

	"gorm.io/gorm"
)

// User is a platform account. Platform credentials are bcrypt-hashed.
type User struct {
	ID           uint      `gorm:"primarykey" json:"id" example:"1"`
	Username     string    `gorm:"uniqueIndex;size:64;not null" json:"username" example:"testuser"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session represents a login token; the token itself is stored hashed.
type Session struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	TokenHash string    `gorm:"uniqueIndex;size:128;not null" json:"-"`
	UserID    uint      `gorm:"index;not null" json:"user_id" example:"1"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// TestSet is the top-level container of imports, units, and flows.
type TestSet struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	Name      string    `gorm:"size:128;not null" json:"name" example:"我的测试集"`
	OwnerID   uint      `gorm:"index;not null" json:"owner_id" example:"1"`
	Host      string    `gorm:"size:255" json:"host" example:"https://api.example.com"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Role constants for TestSetMember.
const (
	RoleRead = "read"
	RoleEdit = "edit"
)

// TestSetMember links a user to a test set with a role (read|edit).
// The owner is recorded on TestSet.OwnerID and implied as full access.
type TestSetMember struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	TestSetID uint      `gorm:"not null" json:"test_set_id" example:"1"`
	UserID    uint      `gorm:"not null" json:"user_id" example:"2"`
	Role      string    `gorm:"size:16;not null" json:"role" enum:"read,edit" example:"edit"`
	CreatedAt time.Time `json:"created_at"`
}

// Import records one swagger document imported into a test set.
type Import struct {
	ID         uint      `gorm:"primarykey" json:"id" example:"1"`
	TestSetID  uint      `gorm:"index;not null" json:"test_set_id" example:"1"`
	Source     string    `gorm:"size:255" json:"source" example:"my-api"`
	RawSwagger string    `gorm:"type:text" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}

// Provider is a user-scoped OpenAI-compatible LLM provider.
type Provider struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	UserID    uint      `gorm:"index;not null" json:"user_id" example:"1"`
	Name      string    `gorm:"size:128;not null" json:"name" example:"我的 OpenAI"`
	BaseURL   string    `gorm:"size:255;not null" json:"base_url" example:"https://api.openai.com"`
	APIKeyEnc string    `gorm:"size:1024" json:"-"`
	Model     string    `gorm:"size:128" json:"model" example:"gpt-4"`
	Enabled   bool      `json:"enabled" example:"true"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TestUnit is one interface derived from a swagger import. Deletion is soft
// (DeletedAt set) so historical flow versions keep working.
type TestUnit struct {
	ID          uint           `gorm:"primarykey" json:"id" example:"1"`
	TestSetID   uint           `gorm:"index;not null" json:"test_set_id" example:"1"`
	ImportID    uint           `json:"import_id" example:"1"`
	Method      string         `gorm:"size:16;not null" json:"method" example:"GET"`
	Path        string         `gorm:"size:512;not null" json:"path" example:"/users/{id}"`
	Slug        string         `gorm:"size:512;index" json:"slug" example:"get-users-{id}"`
	Tag         string         `gorm:"size:128;index" json:"tag" example:"用户"`
	Name        string         `gorm:"size:255" json:"name" example:"获取用户"`
	Params      string         `gorm:"type:text" json:"params"`
	RequestBody string         `gorm:"type:text" json:"request_body"`
	Responses   string         `gorm:"type:text" json:"responses"`
	Security    string         `gorm:"type:text" json:"security"`
	Spec        string         `gorm:"type:text" json:"spec"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at" swaggertype:"primitive,string"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TestFlow is the top-level container of a test flow within a test set.
// It only carries metadata; the executable tree lives in drafts and versions.
type TestFlow struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	TestSetID uint      `gorm:"index;not null" json:"test_set_id" example:"1"`
	Name      string    `gorm:"size:128;not null" json:"name" example:"我的测试流"`
	CreatedBy uint      `json:"created_by" example:"1"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FlowDraft is the editable working state of a flow: a whole-tree JSON snapshot.
type FlowDraft struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	FlowID    uint      `gorm:"index;not null" json:"flow_id" example:"1"`
	Name      string    `gorm:"size:128" json:"name" example:"我的测试流"`
	Tree      string    `gorm:"type:text" json:"tree"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FlowVersion is an immutable self-contained snapshot of the whole tree.
type FlowVersion struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	FlowID    uint      `gorm:"index;not null" json:"flow_id" example:"1"`
	VersionNo int       `gorm:"not null" json:"version_no" example:"1"`
	Tree      string    `gorm:"type:text" json:"tree"`
	Enabled   bool      `gorm:"index" json:"enabled" example:"true"`
	CreatedBy uint      `json:"created_by" example:"1"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionStatus enumerates the states of an agent dialog session.
const (
	SessionActive = "active"
	SessionPaused = "paused"
)

// FlowSession persists the agent dialog history for one flow, keeping context
// across user submissions. Messages is an OpenAI-compatible message array;
// PendingQuestions is a JSON list of pause-point questions awaiting answers.
type FlowSession struct {
	ID               uint      `gorm:"primarykey" json:"id" example:"1"`
	FlowID           uint      `gorm:"index;not null" json:"flow_id" example:"1"`
	CreatedBy        uint      `json:"created_by" example:"1"`
	Status           string    `gorm:"size:16;not null;default:active" json:"status" enum:"active,paused" example:"active"`
	Messages         string    `gorm:"type:text" json:"messages"`
	PendingQuestions string    `gorm:"type:text" json:"pending_questions,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ExecutionLog records one run of a flow. version_id is 0 for a draft trial
// run (restored from the stored tree); otherwise it references the executed
// version whose snapshot restores the exact flow shape.
type ExecutionLog struct {
	ID          uint      `gorm:"primarykey" json:"id" example:"1"`
	FlowID      uint      `gorm:"index;not null" json:"flow_id" example:"1"`
	VersionID   uint      `gorm:"index" json:"version_id" example:"1"`
	VersionNo   int       `json:"version_no" example:"1"`
	Status      string    `gorm:"size:16;not null" json:"status" example:"success"`
	Tree        string    `gorm:"type:text" json:"tree"`
	NodeResults string    `gorm:"type:text" json:"node_results"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// FlowSchedule records a cron trigger for a flow. Enabled controls whether the
// scheduler fires it; JobID is the opaque handle returned by the scheduling
// backend for the currently-registered job.
type FlowSchedule struct {
	ID        uint      `gorm:"primarykey" json:"id" example:"1"`
	FlowID    uint      `gorm:"index;not null" json:"flow_id" example:"1"`
	TestSetID uint      `gorm:"index;not null" json:"test_set_id" example:"1"`
	Cron      string    `gorm:"size:64;not null" json:"cron" example:"0 */2 * * *"`
	Enabled   bool      `gorm:"not null;default:true" json:"enabled" example:"true"`
	JobID     string    `gorm:"size:128" json:"job_id" example:"job_abc123"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AutoMigrate creates all tables for the foundation data layer.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{}, &Session{}, &TestSet{}, &TestSetMember{},
		&Import{}, &Provider{}, &TestUnit{},
		&TestFlow{}, &FlowDraft{}, &FlowVersion{},
		&ExecutionLog{}, &FlowSession{}, &FlowSchedule{},
	)
}
