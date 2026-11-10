package migrations

import (
	"context"

	ormstore "migration-orm"
	sqlstore "passport-session-sql"
)

func init() {
	register(ormstore.Migration{
		Name: "2026_11_10_000003_create_passport_refresh_tokens_table",
		Up: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.Create(ctx, sqlstore.DefaultRefreshTable, sqlstore.RefreshTable)
		},
		Down: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.DropIfExists(ctx, sqlstore.DefaultRefreshTable)
		},
	})
}
