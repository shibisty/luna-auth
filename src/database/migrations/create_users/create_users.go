package create_users

import "database/sql"

func Up(db *sql.DB, prefix string) error {
    query := `
CREATE TABLE IF NOT EXISTS ` + prefix + `users
(
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
    _, err := db.Exec(query)
    return err
}
