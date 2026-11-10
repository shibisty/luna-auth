// Package users registers and authenticates the end users of client
// applications. Every operation is scoped by the application id that the
// request's API key belongs to.
package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"fullstack"
	database "fullstack-orm"
	"orm"
	"passport"
	"passport/passwordhash"

	"luna-auth/app/models"
)

var (
	// ErrInvalidCredentials: the login or the password is wrong (not told apart).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrNotFound: no such user in this application.
	ErrNotFound = errors.New("user not found")
	// ErrInvalidReset: the reset token is unknown, used or expired.
	ErrInvalidReset = errors.New("invalid or expired reset token")
)

// TakenError is returned when an email or username is already used in the
// application.
type TakenError struct{ Field string }

func (e *TakenError) Error() string { return "the " + e.Field + " has already been taken" }

// FieldError is an invalid input field.
type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// ResetTTL is how long a password reset token works.
const ResetTTL = 30 * time.Minute

// Service is in the container (Provider).
type Service struct {
	app *fullstack.App
	now func() time.Time
}

// Provider registers *users.Service.
type Provider struct{}

// Register implements fullstack.Provider.
func (Provider) Register(app *fullstack.App) error {
	fullstack.Provide(app, func(app *fullstack.App) (*Service, error) { return &Service{app: app, now: time.Now}, nil })
	return nil
}

func (s *Service) repo() (*orm.Repository[models.User], error) {
	return database.Repo[models.User](s.app)
}

// Identity is the passport identity of a user, as it goes into tokens.
func Identity(u *models.User) *passport.Identity {
	id := &passport.Identity{Provider: "local", ProviderUserID: u.PublicID, Email: u.Email, EmailVerified: u.EmailVerified}
	if u.Username != nil {
		id.Name = *u.Username
	}
	return id
}

// NormEmail and NormUsername are the stored, compared forms.
func NormEmail(s string) string    { return strings.ToLower(strings.TrimSpace(s)) }
func NormUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// CheckUsername allows 3–32 of a–z, 0–9, ".", "_" and "-", starting with a
// letter or a digit; never "@", so a login is an email exactly when it has one.
func CheckUsername(u string) error {
	if len(u) < 3 || len(u) > 32 {
		return &FieldError{"username", "The username must be 3 to 32 characters."}
	}
	for i, c := range u {
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || (i > 0 && (c == '.' || c == '_' || c == '-'))
		if !ok {
			return &FieldError{"username", "The username may contain only letters, digits, \".\", \"_\" and \"-\", and must start with a letter or a digit."}
		}
	}
	return nil
}

// Register creates a user of the application.
func (s *Service) Register(ctx context.Context, appID, username, email, password string) (*models.User, error) {
	email = NormEmail(email)
	var uname *string
	if u := NormUsername(username); u != "" {
		if err := CheckUsername(u); err != nil {
			return nil, err
		}
		uname = &u
	}
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	if taken, err := repo.Where("app_id", "=", appID).Where("email", "=", email).Exists(ctx); err != nil {
		return nil, err
	} else if taken {
		return nil, &TakenError{"email"}
	}
	if uname != nil {
		if taken, err := repo.Where("app_id", "=", appID).Where("username", "=", *uname).Exists(ctx); err != nil {
			return nil, err
		} else if taken {
			return nil, &TakenError{"username"}
		}
	}
	hash, err := passwordhash.Hash(password)
	if err != nil {
		return nil, err
	}
	u := &models.User{PublicID: newUUID(), AppID: appID, Username: uname, Email: email, PasswordHash: hash}
	if err := repo.Create(ctx, u); err != nil {
		if isUniqueViolation(err) { // a concurrent registration won
			return nil, &TakenError{"email"}
		}
		return nil, err
	}
	return u, nil
}

// Authenticate checks a login (an email when it contains "@", otherwise a
// username) and a password. Unknown logins cost as much time as wrong
// passwords, so the answer does not reveal which accounts exist.
func (s *Service) Authenticate(ctx context.Context, appID, login, password string) (*models.User, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	q := repo.Where("app_id", "=", appID)
	if strings.Contains(login, "@") {
		q = q.Where("email", "=", NormEmail(login))
	} else {
		q = q.Where("username", "=", NormUsername(login))
	}
	u, err := q.First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		passwordhash.Verify(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := passwordhash.Verify(password, u.PasswordHash)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	if passwordhash.NeedsRehash(u.PasswordHash) {
		if h, err := passwordhash.Hash(password); err == nil {
			u.PasswordHash = h
			_ = repo.Update(ctx, u) // best effort
		}
	}
	return u, nil
}

var dummyHash, _ = passwordhash.Hash("not a real password")

// Find returns the application's user with the public id.
func (s *Service) Find(ctx context.Context, appID, publicID string) (*models.User, error) {
	if !validUUID(publicID) {
		return nil, ErrNotFound
	}
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	u, err := repo.Where("app_id", "=", appID).Where("public_id", "=", strings.ToLower(publicID)).First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		return nil, ErrNotFound
	}
	return u, err
}

// List is a page of the application's users, newest first.
func (s *Service) List(ctx context.Context, appID string, page, perPage int) (orm.Page[models.User], error) {
	repo, err := s.repo()
	if err != nil {
		return orm.Page[models.User]{}, err
	}
	return repo.Where("app_id", "=", appID).OrderByDesc("id").Paginate(ctx, page, perPage)
}

// Changes are the fields an administrator may change; nil leaves a field.
type Changes struct {
	Username      *string // "" removes the username
	Email         *string
	EmailVerified *bool
	Password      *string
}

// Update applies changes to a user.
func (s *Service) Update(ctx context.Context, u *models.User, ch Changes) error {
	repo, err := s.repo()
	if err != nil {
		return err
	}
	if ch.Email != nil {
		email := NormEmail(*ch.Email)
		if email != u.Email {
			if taken, err := repo.Where("app_id", "=", u.AppID).Where("email", "=", email).Exists(ctx); err != nil {
				return err
			} else if taken {
				return &TakenError{"email"}
			}
			u.Email = email
			u.EmailVerified = false
		}
	}
	if ch.Username != nil {
		uname := NormUsername(*ch.Username)
		switch {
		case uname == "":
			u.Username = nil
		case u.Username == nil || *u.Username != uname:
			if err := CheckUsername(uname); err != nil {
				return err
			}
			if taken, err := repo.Where("app_id", "=", u.AppID).Where("username", "=", uname).Exists(ctx); err != nil {
				return err
			} else if taken {
				return &TakenError{"username"}
			}
			u.Username = &uname
		}
	}
	if ch.EmailVerified != nil {
		u.EmailVerified = *ch.EmailVerified
	}
	if ch.Password != nil {
		h, err := passwordhash.Hash(*ch.Password)
		if err != nil {
			return err
		}
		u.PasswordHash = h
	}
	if err := repo.Update(ctx, u); err != nil {
		if isUniqueViolation(err) {
			return &TakenError{"email"}
		}
		return err
	}
	return nil
}

// Delete deletes a user (and their reset tokens).
func (s *Service) Delete(ctx context.Context, u *models.User) error {
	repo, err := s.repo()
	if err != nil {
		return err
	}
	return repo.Delete(ctx, u.ID)
}

// StartReset makes a password reset token for the application's user with
// the email. It returns ErrNotFound for an unknown email; the caller must
// not tell the client.
func (s *Service) StartReset(ctx context.Context, appID, email string) (*models.User, string, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, "", err
	}
	u, err := repo.Where("app_id", "=", appID).Where("email", "=", NormEmail(email)).First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	resets, err := database.Repo[models.PasswordReset](s.app)
	if err != nil {
		return nil, "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	r := &models.PasswordReset{UserID: u.ID, TokenHash: tokenHash(token), ExpiresAt: s.now().UTC().Add(ResetTTL)}
	if err := resets.Create(ctx, r); err != nil {
		return nil, "", err
	}
	return u, token, nil
}

// Reset sets a new password with a reset token of the application's user;
// the token is spent, and so are the user's other reset tokens.
func (s *Service) Reset(ctx context.Context, appID, token, password string) (*models.User, error) {
	resets, err := database.Repo[models.PasswordReset](s.app)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	r, err := resets.Where("token_hash", "=", tokenHash(token)).WhereNull("used_at").Where("expires_at", ">", now).First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		return nil, ErrInvalidReset
	}
	if err != nil {
		return nil, err
	}
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	u, err := repo.Where("id", "=", r.UserID).Where("app_id", "=", appID).First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		return nil, ErrInvalidReset // another application's token
	}
	if err != nil {
		return nil, err
	}
	if err := s.Update(ctx, u, Changes{Password: &password}); err != nil {
		return nil, err
	}
	if _, err := resets.Where("user_id", "=", u.ID).WhereNull("used_at").Update(ctx, map[string]any{"used_at": now}); err != nil {
		return nil, err
	}
	return u, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "SQLSTATE 23505") || strings.Contains(err.Error(), "duplicate key"))
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
