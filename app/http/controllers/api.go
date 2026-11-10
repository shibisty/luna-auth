// Package controllers holds the API handlers. Every handler runs behind
// keys.RequireKey: the key in the context names the application.
package controllers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fullstack"
	"fullstack-validation"
	"fullstack/web"
	"luna-keys"
	"passport/jwt"
	"passport/ratelimit"
	"passport/refresh"

	"luna-auth/app/mail"
	"luna-auth/app/models"
	"luna-auth/app/tokens"
	"luna-auth/app/users"
)

// API serves /v1.
type API struct {
	App *fullstack.App
	// Limit counts logins, registrations and reset requests per application
	// and client address.
	Limit ratelimit.Limiter
}

// NewAPI makes the handlers with the default limit: 10 attempts a minute
// per application and client address.
func NewAPI(app *fullstack.App) API {
	return API{App: app, Limit: ratelimit.New(10, time.Minute)}
}

// UserJSON is a user as the API shows it.
type UserJSON struct {
	ID            string    `json:"id"`
	Username      *string   `json:"username"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func userJSON(u *models.User) UserJSON {
	return UserJSON{ID: u.PublicID, Username: u.Username, Email: u.Email, EmailVerified: u.EmailVerified,
		CreatedAt: u.CreatedAt.UTC(), UpdatedAt: u.UpdatedAt.UTC()}
}

type session struct {
	User UserJSON `json:"user"`
	refresh.Pair
}

func (h API) users() (*users.Service, error)   { return fullstack.Make[*users.Service](h.App) }
func (h API) tokens() (*tokens.Service, error) { return fullstack.Make[*tokens.Service](h.App) }

// appID is the application of the request's key.
func appID(c *web.Ctx) string {
	k, _ := keys.FromContext(c.Context())
	return k.AppID
}

func invalid(field, msg string) error {
	return validation.Invalid(validation.Errors{field: {msg}})
}

func userError(err error) error {
	var taken *users.TakenError
	if errors.As(err, &taken) {
		return invalid(taken.Field, "The "+taken.Field+" has already been taken.")
	}
	var fe *users.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Message)
	}
	return err
}

// throttle refuses a request beyond the limit with 429 and Retry-After.
func (h API) throttle(c *web.Ctx, action string) error {
	host, _, err := net.SplitHostPort(c.R.RemoteAddr)
	if err != nil {
		host = c.R.RemoteAddr
	}
	ok, wait := h.Limit.Allow(action + "|" + appID(c) + "|" + host)
	if ok {
		return nil
	}
	c.W.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	return web.Error(http.StatusTooManyRequests, "Too many attempts. Try again later.")
}

type registerIn struct {
	Username string `json:"username" validate:"max=32"`
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=200"`
}

// Register is POST /v1/users: creates a user and logs them in.
func (h API) Register(c *web.Ctx) error {
	if err := h.throttle(c, "register"); err != nil {
		return err
	}
	var in registerIn
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
	return h.login(c, http.StatusCreated, u)
}

type loginIn struct {
	Login    string `json:"login" validate:"required,max=254"` // a username or an email
	Password string `json:"password" validate:"required,max=200"`
}

// Login is POST /v1/sessions: a username or an email and a password.
func (h API) Login(c *web.Ctx) error {
	if err := h.throttle(c, "login"); err != nil {
		return err
	}
	var in loginIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	u, err := svc.Authenticate(c.Context(), appID(c), in.Login, in.Password)
	if errors.Is(err, users.ErrInvalidCredentials) {
		return web.Error(http.StatusUnauthorized, "These credentials do not match our records.")
	}
	if err != nil {
		return err
	}
	return h.login(c, http.StatusOK, u)
}

func (h API) login(c *web.Ctx, status int, u *models.User) error {
	ts, err := h.tokens()
	if err != nil {
		return err
	}
	pair, err := ts.Issue(c.Context(), appID(c), users.Identity(u))
	if err != nil {
		return err
	}
	c.W.Header().Set("Cache-Control", "no-store")
	return c.JSON(status, session{User: userJSON(u), Pair: pair})
}

type refreshIn struct {
	RefreshToken string `json:"refresh_token" validate:"required,max=200"`
}

// Refresh is POST /v1/tokens/refresh: a new pair for a refresh token, which
// is spent. Presenting a spent token again ends that login everywhere.
func (h API) Refresh(c *web.Ctx) error {
	var in refreshIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	ts, err := h.tokens()
	if err != nil {
		return err
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	belongs := func(ctx context.Context, app, userID string) (bool, error) {
		_, err := svc.Find(ctx, app, userID)
		if errors.Is(err, users.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	pair, err := ts.Refresh(c.Context(), appID(c), in.RefreshToken, belongs)
	if errors.Is(err, refresh.ErrInvalid) || errors.Is(err, refresh.ErrReused) || errors.Is(err, tokens.ErrWrongApp) {
		return web.Error(http.StatusUnauthorized, "Invalid or expired refresh token. Log in again.")
	}
	if err != nil {
		return err
	}
	c.W.Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, pair)
}

// Revoke is POST /v1/tokens/revoke: logs out (the refresh token's login).
// It answers 204 for unknown tokens too.
func (h API) Revoke(c *web.Ctx) error {
	var in refreshIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	ts, err := h.tokens()
	if err != nil {
		return err
	}
	if err := ts.Revoke(c.Context(), in.RefreshToken); err != nil && !errors.Is(err, refresh.ErrInvalid) {
		return err
	}
	return c.NoContent()
}

// Me is GET /v1/me: the user of the access token in Authorization: Bearer.
func (h API) Me(c *web.Ctx) error {
	u, err := h.bearer(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userJSON(u))
}

// bearer is the user of the access token in Authorization: Bearer.
func (h API) bearer(c *web.Ctx) (*models.User, error) {
	scheme, token, ok := strings.Cut(c.R.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		c.W.Header().Set("WWW-Authenticate", `Bearer realm="luna-auth"`)
		return nil, web.Error(http.StatusUnauthorized, "Send the user's access token in Authorization: Bearer.")
	}
	return h.verified(c, token)
}

func (h API) verified(c *web.Ctx, token string) (*models.User, error) {
	ts, err := h.tokens()
	if err != nil {
		return nil, err
	}
	id, err := ts.Verify(c.Context(), appID(c), strings.TrimSpace(token))
	if err != nil {
		var authErr bool
		for _, e := range []error{jwt.ErrExpired, jwt.ErrBadAudience, jwt.ErrBadIssuer, jwt.ErrMalformedToken, jwt.ErrBadSignature, jwt.ErrMissingSubject, jwt.ErrNotYetValid, jwt.ErrRevoked, jwt.ErrBadAlgorithm, jwt.ErrBadType, jwt.ErrUnknownKey} {
			authErr = authErr || errors.Is(err, e)
		}
		if authErr {
			return nil, web.Error(http.StatusUnauthorized, "Invalid or expired access token.")
		}
		return nil, err
	}
	svc, err := h.users()
	if err != nil {
		return nil, err
	}
	u, err := svc.Find(c.Context(), appID(c), id.ProviderUserID)
	if errors.Is(err, users.ErrNotFound) {
		return nil, web.Error(http.StatusUnauthorized, "The user no longer exists.")
	}
	return u, err
}

type verifyIn struct {
	Token string `json:"token" validate:"required,max=4096"`
}

// Verify is POST /v1/tokens/verify, for a client's backend: whether an
// access token is valid for this application, and whose it is (RFC 7662
// style "active").
func (h API) Verify(c *web.Ctx) error {
	var in verifyIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	u, err := h.verified(c, in.Token)
	var he *web.HTTPError
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
		return c.JSON(http.StatusOK, map[string]any{"active": false})
	}
	if err != nil {
		return err
	}
	ts, _ := h.tokens()
	exp, _ := ts.Expires(c.Context(), appID(c), in.Token)
	return c.JSON(http.StatusOK, map[string]any{"active": true, "sub": u.PublicID, "exp": exp.Unix(), "user": userJSON(u)})
}

type forgotIn struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

// Forgot is POST /v1/password/forgot: emails a reset link if the email
// belongs to a user, and answers 202 either way.
func (h API) Forgot(c *web.Ctx) error {
	if err := h.throttle(c, "forgot"); err != nil {
		return err
	}
	var in forgotIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	k, _ := keys.FromContext(c.Context())
	if k.PasswordResetURL == "" {
		return web.Error(http.StatusConflict, "The application has no password reset URL; set it in the Luna dashboard.")
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	u, token, err := svc.StartReset(c.Context(), k.AppID, in.Email)
	if errors.Is(err, users.ErrNotFound) {
		return c.JSON(http.StatusAccepted, map[string]string{"status": "If the email belongs to an account, a reset link is on its way."})
	}
	if err != nil {
		return err
	}
	link, err := resetLink(k.PasswordResetURL, token)
	if err != nil {
		return err
	}
	m, err := fullstack.Make[mail.Mailer](h.App)
	if err != nil {
		return err
	}
	name := k.AppName
	if name == "" {
		name = "your account"
	}
	err = m.Send(c.Context(), mail.Message{To: u.Email, Subject: "Reset your password for " + name,
		Body: "Someone asked to reset the password of " + name + " for this email.\n\n" +
			"Open this link within " + strconv.Itoa(int(users.ResetTTL.Minutes())) + " minutes to choose a new password:\n\n" +
			link + "\n\nIf it was not you, ignore this email: your password stays as it is.\n"})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, map[string]string{"status": "If the email belongs to an account, a reset link is on its way."})
}

func resetLink(base, token string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type resetIn struct {
	Token    string `json:"token" validate:"required,max=200"`
	Password string `json:"password" validate:"required,min=8,max=200"`
}

// Reset is POST /v1/password/reset: a new password with a reset token. Every
// login of the user ends.
func (h API) Reset(c *web.Ctx) error {
	var in resetIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	u, err := svc.Reset(c.Context(), appID(c), in.Token, in.Password)
	if errors.Is(err, users.ErrInvalidReset) {
		return invalid("token", "The reset link is invalid or has expired. Ask for a new one.")
	}
	if err != nil {
		return err
	}
	ts, err := h.tokens()
	if err != nil {
		return err
	}
	if err := ts.RevokeUser(c.Context(), u.PublicID); err != nil {
		return err
	}
	return c.NoContent()
}

// ---- administration: server keys with auth:users.read / auth:users.write ----

// ListUsers is GET /v1/users?q=&page=&per_page=: q searches emails and usernames.
func (h API) ListUsers(c *web.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page"))
	per, _ := strconv.Atoi(c.Query("per_page"))
	if page < 1 {
		page = 1
	}
	if per < 1 || per > 100 {
		per = 25
	}
	svc, err := h.users()
	if err != nil {
		return err
	}
	p, err := svc.List(c.Context(), appID(c), c.Query("q"), page, per)
	if err != nil {
		return err
	}
	data := make([]UserJSON, len(p.Items))
	for i := range p.Items {
		data[i] = userJSON(&p.Items[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"data": data, "page": p.Page, "per_page": p.PerPage, "total": p.Total, "last_page": p.LastPage})
}

func (h API) user(c *web.Ctx) (*users.Service, *models.User, error) {
	svc, err := h.users()
	if err != nil {
		return nil, nil, err
	}
	u, err := svc.Find(c.Context(), appID(c), c.Param("id"))
	if errors.Is(err, users.ErrNotFound) {
		return nil, nil, web.ErrNotFound
	}
	return svc, u, err
}

// ShowUser is GET /v1/users/{id}.
func (h API) ShowUser(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userJSON(u))
}

type updateIn struct {
	Username      *string `json:"username" validate:"omitempty,max=32"`
	Email         *string `json:"email" validate:"omitempty,email,max=254"`
	EmailVerified *bool   `json:"email_verified"`
	Password      *string `json:"password" validate:"omitempty,min=8,max=200"`
}

// UpdateUser is PATCH /v1/users/{id}: the fields sent change; a new
// password ends the user's logins.
func (h API) UpdateUser(c *web.Ctx) error {
	svc, u, err := h.user(c)
	if err != nil {
		return err
	}
	var in updateIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	if err := svc.Update(c.Context(), u, users.Changes{Username: in.Username, Email: in.Email, EmailVerified: in.EmailVerified, Password: in.Password}); err != nil {
		return userError(err)
	}
	if in.Password != nil {
		ts, err := h.tokens()
		if err != nil {
			return err
		}
		if err := ts.RevokeUser(c.Context(), u.PublicID); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, userJSON(u))
}

// DeleteUser is DELETE /v1/users/{id}: the user and every login of theirs.
func (h API) DeleteUser(c *web.Ctx) error {
	svc, u, err := h.user(c)
	if err != nil {
		return err
	}
	ts, err := h.tokens()
	if err != nil {
		return err
	}
	if err := ts.RevokeUser(c.Context(), u.PublicID); err != nil {
		return err
	}
	if err := svc.Delete(c.Context(), u); err != nil {
		return err
	}
	return c.NoContent()
}
