package controllers

import (
	"net/http"

	"fullstack-validation"
	"fullstack/web"
	"luna-keys"

	"luna-auth/app/users"
)

// Internal guards the dashboard's administration API,
// /internal/v1/apps/{app}/…: the dashboard's service token in
// Authorization: Bearer (the same secret luna-auth uses towards the
// dashboard, LUNA_DASHBOARD_TOKEN). The application comes from the path,
// so the handlers of /v1 run unchanged, as if a server key of that
// application with every auth scope had called. The dashboard checks that
// the signed-in developer owns the application before it calls.
func Internal(token string) web.Middleware {
	return keys.Internal(token, keys.AuthLogin, keys.AuthUsersRead, keys.AuthUsersWrite, keys.AuthDataRead, keys.AuthDataWrite)
}

type createUserIn struct {
	Username      string `json:"username" validate:"max=32"`
	Email         string `json:"email" validate:"required,email,max=254"`
	Password      string `json:"password" validate:"required,min=8,max=200"`
	EmailVerified bool   `json:"email_verified"`
}

// CreateUser is POST /internal/v1/apps/{app}/users: the developer adds a
// user from the dashboard (no login, no tokens).
func (h API) CreateUser(c *web.Ctx) error {
	var in createUserIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	u, err := svc.Register(c.Context(), appID(c), in.Username, in.Email, in.Password)
	if err != nil {
		return userError(err)
	}
	if in.EmailVerified {
		v := true
		if err := svc.Update(c.Context(), u, users.Changes{EmailVerified: &v}); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusCreated, userJSON(u))
}
