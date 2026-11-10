package controllers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

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
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, got, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), want) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="luna-auth internal"`)
				internalDeny(w, http.StatusUnauthorized, "missing or invalid service token")
				return
			}
			app := strings.ToLower(r.PathValue("app"))
			if !uuidLike(app) {
				internalDeny(w, http.StatusNotFound, "no such application")
				return
			}
			k := &keys.Key{AppID: app, Kind: keys.Server, Scopes: []keys.Scope{
				keys.AuthLogin, keys.AuthUsersRead, keys.AuthUsersWrite, keys.AuthDataRead, keys.AuthDataWrite,
			}}
			next.ServeHTTP(w, r.WithContext(keys.WithKey(r.Context(), k)))
		})
	}
}

func internalDeny(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func uuidLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f'):
			return false
		}
	}
	return true
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
