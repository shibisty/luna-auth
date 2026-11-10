// Package models holds the application's models.
package models

import "orm"

// User is an end user of one client application. Emails and usernames
// are unique per application and stored lower-case.
type User struct {
	orm.BaseModel
	PublicID      string  `db:"public_id"` // uuid: the user's id in the API and in tokens (sub)
	AppID         string  `db:"app_id"`    // the dashboard's application id
	Username      *string `db:"username"`  // optional
	Email         string  `db:"email"`
	PasswordHash  string  `db:"password_hash"` // passport/passwordhash, never the password
	EmailVerified bool    `db:"email_verified"`
	orm.Timestamps
}

// TableName implements orm.Model.
func (User) TableName() string { return "users" }
