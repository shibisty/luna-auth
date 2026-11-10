package migrations

import (
	"context"

	ormstore "migration-orm"
	"orm/schema"
)

func init() {
	register(ormstore.Migration{
		Name: "2026_11_10_000002_create_password_resets_table",
		Up: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.Create(ctx, "password_resets", func(t *schema.Blueprint) {
				t.ID()
				t.ForeignID("user_id").References("id").On("users").OnDelete("cascade")
				t.Char("token_hash", 64).Unique()
				t.Timestamp("expires_at")
				t.Timestamp("used_at").Nullable()
				t.Timestamps()
				t.IndexCols("user_id")
			})
		},
		Down: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.DropIfExists(ctx, "password_resets")
		},
	})
}
