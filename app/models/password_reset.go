package models

import (
	"time"

	"orm"
)

// PasswordReset is a single-use password reset token; only its SHA-256 is
// kept.
type PasswordReset struct {
	orm.BaseModel
	UserID    int64      `db:"user_id"`
	TokenHash string     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	orm.Timestamps
}

// TableName implements orm.Model.
func (PasswordReset) TableName() string { return "password_resets" }
