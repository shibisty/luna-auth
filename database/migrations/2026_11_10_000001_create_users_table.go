package migrations

import (
	"context"

	ormstore "migration-orm"
	"orm/schema"
)

func init() {
	register(ormstore.Migration{
		Name: "2026_11_10_000001_create_users_table",
		Up: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.Create(ctx, "users", func(t *schema.Blueprint) {
				t.ID()
				t.UUID("public_id").Unique()
				t.UUID("app_id")
				t.String("username", 32).Nullable()
				t.String("email", 254)
				t.String("password_hash")
				t.Boolean("email_verified").Default(false)
				t.Timestamps()
				t.UniqueCols("app_id", "email")
				t.UniqueCols("app_id", "username") // NULL usernames do not collide
			})
		},
		Down: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.DropIfExists(ctx, "users")
		},
	})
}
