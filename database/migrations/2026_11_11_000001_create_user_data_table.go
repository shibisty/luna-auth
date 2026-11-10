package migrations

import (
	"context"

	ormstore "migration-orm"
	"orm/schema"
)

func init() {
	register(ormstore.Migration{
		Name: "2026_11_11_000001_create_user_data_table",
		Up: func(ctx context.Context, db *ormstore.DB) error {
			// The application's own data about a user: string values by key
			// (app/userdata). Deleted with the user.
			return db.Schema.Create(ctx, "user_data", func(t *schema.Blueprint) {
				t.ID()
				t.UUID("app_id")
				t.ForeignID("user_id").References("id").On("users").OnDelete("cascade")
				t.String("key", 64)
				t.Text("value")
				t.Timestamps()
				t.UniqueCols("user_id", "key")
				t.IndexCols("app_id", "key")
			})
		},
		Down: func(ctx context.Context, db *ormstore.DB) error {
			return db.Schema.DropIfExists(ctx, "user_data")
		},
	})
}
