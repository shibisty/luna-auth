// Package tokens issues the end users' tokens: a short JWT access token
// whose audience is the application, and a rotating refresh token kept in
// the database (passport/refresh over passport-session-sql).
package tokens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fullstack"
	database "fullstack-orm"
	"fullstack/env"
	"orm"
	"passport"
	sqlstore "passport-session-sql"
	"passport/jwt"
	"passport/refresh"
)

// Issuer is the iss claim of every token.
const Issuer = "luna-auth"

// ErrWrongApp: a refresh token of another application's user.
var ErrWrongApp = errors.New("tokens: the token belongs to another application")

// Config is read from the environment.
type Config struct {
	Secret     string        `env:"AUTH_JWT_SECRET,required"` // 32+ bytes
	AccessTTL  time.Duration `env:"AUTH_TOKEN_TTL" default:"15m"`
	RefreshTTL time.Duration `env:"AUTH_REFRESH_TTL" default:"720h"`
}

// Service is in the container (Provider).
type Service struct {
	app *fullstack.App
	cfg Config
}

// Provider registers *tokens.Service; it fails at start without a secret
// of at least 32 bytes.
type Provider struct{}

// Register implements fullstack.Provider.
func (Provider) Register(app *fullstack.App) error {
	var cfg Config
	if err := env.Decode(&cfg); err != nil {
		return fmt.Errorf("tokens: %w", err)
	}
	if _, err := jwt.New(cfg.Secret, Issuer); err != nil {
		return fmt.Errorf("tokens: AUTH_JWT_SECRET: %w", err)
	}
	s := &Service{app: app, cfg: cfg}
	fullstack.Set(app, s)
	return nil
}

// Config returns the token lifetimes.
func (s *Service) Config() Config { return s.cfg }

// issuer signs and checks the tokens of one application: aud is its id,
// so a token of one application is refused by another.
func (s *Service) issuer(appID string) (*jwt.Issuer, error) {
	return jwt.New(s.cfg.Secret, Issuer, jwt.WithAudience(appID))
}

func (s *Service) refresher() (*refresh.Refresher, error) {
	exec, err := database.Exec(s.app)
	if err != nil {
		return nil, err
	}
	return refresh.New(sqlstore.RefreshTokens(exec, ""), s.cfg.RefreshTTL), nil
}

// Issue starts a login of the application's user: an access token and the
// first refresh token of a new family.
func (s *Service) Issue(ctx context.Context, appID string, id *passport.Identity) (refresh.Pair, error) {
	iss, err := s.issuer(appID)
	if err != nil {
		return refresh.Pair{}, err
	}
	r, err := s.refresher()
	if err != nil {
		return refresh.Pair{}, err
	}
	return r.Issue(ctx, iss, id, s.cfg.AccessTTL)
}

// BelongsTo reports whether a user (by public id) is the application's.
type BelongsTo func(ctx context.Context, appID, userPublicID string) (bool, error)

// Refresh rotates a refresh token for the application. A token of another
// application's user is refused (ErrWrongApp) and spent; a reused token
// revokes its whole family (refresh.ErrReused).
func (s *Service) Refresh(ctx context.Context, appID, token string, belongs BelongsTo) (refresh.Pair, error) {
	iss, err := s.issuer(appID)
	if err != nil {
		return refresh.Pair{}, err
	}
	r, err := s.refresher()
	if err != nil {
		return refresh.Pair{}, err
	}
	return r.Rotate(ctx, checked{iss: iss, appID: appID, belongs: belongs}, token, s.cfg.AccessTTL)
}

// checked signs the new access token only for the application's own user.
type checked struct {
	iss     *jwt.Issuer
	appID   string
	belongs BelongsTo
}

func (c checked) Issue(ctx context.Context, id *passport.Identity, ttl time.Duration) (string, error) {
	ok, err := c.belongs(ctx, c.appID, id.ProviderUserID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrWrongApp
	}
	return c.iss.Issue(ctx, id, ttl)
}

// Revoke ends a login: the refresh token's whole family.
func (s *Service) Revoke(ctx context.Context, token string) error {
	r, err := s.refresher()
	if err != nil {
		return err
	}
	return r.Revoke(ctx, token)
}

// RevokeUser ends every login of a user (after a password reset or a
// deletion). Access tokens already issued live until they expire
// (AUTH_TOKEN_TTL); /v1/me checks that the user still exists.
func (s *Service) RevokeUser(ctx context.Context, userPublicID string) error {
	exec, err := fullstack.Make[orm.SQLExecutor](s.app)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `DELETE FROM `+sqlstore.DefaultRefreshTable+` WHERE (identity::jsonb)->>'provider_user_id' = $1`, userPublicID)
	return err
}

// Verify checks an access token of the application.
func (s *Service) Verify(ctx context.Context, appID, token string) (*passport.Identity, error) {
	iss, err := s.issuer(appID)
	if err != nil {
		return nil, err
	}
	return iss.Verify(ctx, token)
}

// Expires returns when an access token expires, without checking anything
// but its signature and claims.
func (s *Service) Expires(ctx context.Context, appID, token string) (time.Time, error) {
	iss, err := s.issuer(appID)
	if err != nil {
		return time.Time{}, err
	}
	_, exp, err := iss.TokenID(ctx, token)
	return exp, err
}
