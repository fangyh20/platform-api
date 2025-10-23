package models

import (
	"time"
)

// User represents a platform user
type User struct {
	ID            string    `json:"id" db:"id"`
	Email         string    `json:"email" db:"email"`
	PasswordHash  *string   `json:"-" db:"password_hash"` // Never expose password hash, NULL for Google-only accounts
	FullName      string    `json:"full_name" db:"full_name"`
	AvatarURL     *string   `json:"avatar_url,omitempty" db:"avatar_url"`
	EmailVerified bool      `json:"email_verified" db:"email_verified"`
	GoogleID      *string   `json:"google_id,omitempty" db:"google_id"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// App represents a user's application
type App struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"` // draft, building, active, error
	ProdVersion *int      `json:"prod_version" db:"prod_version"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Version represents a version of an app
type Version struct {
	ID             string     `json:"id" db:"id"`
	AppID          string     `json:"app_id" db:"app_id"`
	VersionNumber  int        `json:"version_number" db:"version_number"`
	Status         string     `json:"status" db:"status"` // pending, building, completed, failed, promoted
	S3CodePath     *string    `json:"s3_code_path,omitempty" db:"s3_code_path"`
	VercelURL      *string    `json:"vercel_url,omitempty" db:"vercel_url"`
	VercelDeployID *string    `json:"vercel_deploy_id,omitempty" db:"vercel_deploy_id"`
	BuildLog       *string    `json:"build_log,omitempty" db:"build_log"`
	ErrorMessage   *string    `json:"error_message,omitempty" db:"error_message"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// Comment represents a user comment on an app
type Comment struct {
	ID            string     `json:"id" db:"id"`
	AppID         string     `json:"app_id" db:"app_id"`
	VersionID     *string    `json:"version_id" db:"version_id"` // null until submitted
	UserID        string     `json:"user_id" db:"user_id"`
	PagePath      string     `json:"page_path" db:"page_path"`     // e.g., "/home", "/about"
	ElementPath   string     `json:"element_path" db:"element_path"` // CSS selector or XPath
	Content       string     `json:"content" db:"content"`
	Status        string     `json:"status" db:"status"` // draft, submitted, resolved
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	SubmittedAt   *time.Time `json:"submitted_at" db:"submitted_at"`
}

// RequirementFile represents uploaded requirement files
type RequirementFile struct {
	ID        string    `json:"id" db:"id"`
	AppID     string    `json:"app_id" db:"app_id"`
	VersionID string    `json:"version_id" db:"version_id"`
	FileName  string    `json:"file_name" db:"file_name"`
	FileType  string    `json:"file_type" db:"file_type"` // text, image
	S3Path    string    `json:"s3_path" db:"s3_path"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// CreateAppRequest represents request to create a new app
type CreateAppRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Requirements string   `json:"requirements"`
	Files        []string `json:"files"` // S3 paths of uploaded files
}

// CreateVersionRequest represents request to create a new version
type CreateVersionRequest struct {
	Comments []string `json:"comments"` // Comment IDs to include in this version
}

// AddCommentRequest represents request to add a comment
type AddCommentRequest struct {
	PagePath    string `json:"page_path"`
	ElementPath string `json:"element_path"`
	Content     string `json:"content"`
}

// BuildProgress represents real-time build progress
type BuildProgress struct {
	VersionID string    `json:"version_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
