// Package userdata is each application's own data about its users: string
// values by key, per user. Every application has its own users (users are
// scoped by app_id), so a user's data is always the data of one
// application; the rows carry app_id as well, for queries across the
// application's users.
//
// The store is PostgreSQL-specific (an upsert and a row lock), like the
// rest of luna-auth.
package userdata

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"

	"fullstack"
	"orm"

	"luna-auth/app/models"
	"luna-auth/app/users"
)

// Limits of the store.
const (
	MaxKeys       = 100       // keys per user
	MaxValueBytes = 16 * 1024 // bytes of one value (UTF-8)
	MaxKeyLength  = 64
)

// ErrNotFound: the user has no value under the key.
var ErrNotFound = errors.New("no such key")

var keyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

// Entry is one value.
type Entry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service is in the container (Provider).
type Service struct{ app *fullstack.App }

// Provider registers *userdata.Service.
type Provider struct{}

// Register implements fullstack.Provider.
func (Provider) Register(app *fullstack.App) error {
	fullstack.Provide(app, func(app *fullstack.App) (*Service, error) { return &Service{app: app}, nil })
	return nil
}

func (s *Service) db() (orm.SQLExecutor, error) { return fullstack.Make[orm.SQLExecutor](s.app) }

// CheckKey allows 1–64 of A–Z, a–z, 0–9, "_", ".", ":" and "-", starting
// with a letter or a digit ("profile.city", "game:level").
func CheckKey(key string) error {
	if !keyRe.MatchString(key) {
		return &users.FieldError{Field: "key", Message: fmt.Sprintf("The key must be 1 to %d of letters, digits, \"_\", \".\", \":\" and \"-\", starting with a letter or a digit.", MaxKeyLength)}
	}
	return nil
}

// CheckValue allows valid UTF-8 up to MaxValueBytes.
func CheckValue(field, v string) error {
	if len(v) > MaxValueBytes {
		return &users.FieldError{Field: field, Message: fmt.Sprintf("The value may not be longer than %d bytes.", MaxValueBytes)}
	}
	if !utf8.ValidString(v) || containsNUL(v) {
		return &users.FieldError{Field: field, Message: "The value must be text (UTF-8, no NUL characters)."}
	}
	return nil
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}

const columns = `key, value, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	err := row.Scan(&e.Key, &e.Value, &e.CreatedAt, &e.UpdatedAt)
	e.CreatedAt, e.UpdatedAt = e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	return e, err
}

// All returns the user's values, by key.
func (s *Service) All(ctx context.Context, u *models.User) ([]Entry, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return all(ctx, db, u.ID)
}

func all(ctx context.Context, db orm.SQLExecutor, userID int64) ([]Entry, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+columns+` FROM user_data WHERE user_id = $1 ORDER BY key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns one value, ErrNotFound when the key is not set.
func (s *Service) Get(ctx context.Context, u *models.User, key string) (Entry, error) {
	if CheckKey(key) != nil {
		return Entry{}, ErrNotFound
	}
	db, err := s.db()
	if err != nil {
		return Entry{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT `+columns+` FROM user_data WHERE user_id = $1 AND key = $2`, u.ID, key)
	if err != nil {
		return Entry{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrNotFound
	}
	return scan(rows)
}

// Set stores a value under a key; created reports a new key.
func (s *Service) Set(ctx context.Context, u *models.User, key, value string) (e Entry, created bool, err error) {
	if err := CheckKey(key); err != nil {
		return Entry{}, false, err
	}
	if err := CheckValue("value", value); err != nil {
		return Entry{}, false, err
	}
	err = s.tx(ctx, u, func(tx orm.Tx, keys map[string]bool) error {
		if !keys[key] && len(keys) >= MaxKeys {
			return limitError("key")
		}
		e, err = upsert(ctx, tx, u, key, value)
		created = !keys[key]
		return err
	})
	return e, created, err
}

// Patch sets several values at once and deletes the keys whose value is
// nil, all or nothing. It returns every value of the user afterwards.
func (s *Service) Patch(ctx context.Context, u *models.User, changes map[string]*string) ([]Entry, error) {
	names := make([]string, 0, len(changes))
	for k, v := range changes {
		if err := CheckKey(k); err != nil {
			return nil, &users.FieldError{Field: "data", Message: fmt.Sprintf("%q: %s", k, err.(*users.FieldError).Message)}
		}
		if v != nil {
			if err := CheckValue("data", *v); err != nil {
				return nil, &users.FieldError{Field: "data", Message: fmt.Sprintf("%q: %s", k, err.(*users.FieldError).Message)}
			}
		}
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Entry
	err := s.tx(ctx, u, func(tx orm.Tx, keys map[string]bool) error {
		n := len(keys)
		for _, k := range names {
			switch v := changes[k]; {
			case v == nil && keys[k]:
				n--
			case v != nil && !keys[k]:
				n++
			}
		}
		if n > MaxKeys {
			return limitError("data")
		}
		for _, k := range names {
			if v := changes[k]; v == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM user_data WHERE user_id = $1 AND key = $2`, u.ID, k); err != nil {
					return err
				}
			} else if _, err := upsert(ctx, tx, u, k, *v); err != nil {
				return err
			}
		}
		var err error
		out, err = all(ctx, tx, u.ID)
		return err
	})
	return out, err
}

// Delete removes a key, ErrNotFound when it was not set.
func (s *Service) Delete(ctx context.Context, u *models.User, key string) error {
	if CheckKey(key) != nil {
		return ErrNotFound
	}
	db, err := s.db()
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM user_data WHERE user_id = $1 AND key = $2`, u.ID, key)
	if err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func limitError(field string) error {
	return &users.FieldError{Field: field, Message: fmt.Sprintf("A user may have at most %d keys.", MaxKeys)}
}

// tx runs fn in a transaction that holds the user's row lock (so the key
// limit holds under concurrent writes), with the user's current keys.
func (s *Service) tx(ctx context.Context, u *models.User, fn func(tx orm.Tx, keys map[string]bool) error) (err error) {
	db, err := s.db()
	if err != nil {
		return err
	}
	// Inside a transaction already (database.UseTransaction in tests): join
	// it; its owner commits or rolls back.
	tx, joined := db.(orm.Tx)
	if !joined {
		if tx, err = db.BeginTx(ctx); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, u.ID)
	if err != nil {
		return err
	}
	found := rows.Next()
	rows.Close()
	if !found {
		return users.ErrNotFound
	}
	rows, err = tx.QueryContext(ctx, `SELECT key FROM user_data WHERE user_id = $1`, u.ID)
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		keys[k] = true
	}
	rows.Close()
	if err := fn(tx, keys); err != nil {
		return err
	}
	if joined {
		return nil
	}
	return tx.Commit()
}

func upsert(ctx context.Context, tx orm.Tx, u *models.User, key, value string) (Entry, error) {
	rows, err := tx.QueryContext(ctx, `INSERT INTO user_data (app_id, user_id, key, value, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now() AT TIME ZONE 'UTC', now() AT TIME ZONE 'UTC')
		ON CONFLICT (user_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now() AT TIME ZONE 'UTC'
		RETURNING `+columns, u.AppID, u.ID, key, value)
	if err != nil {
		return Entry{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Entry{}, err
		}
		return Entry{}, errors.New("userdata: upsert returned no row")
	}
	return scan(rows)
}
