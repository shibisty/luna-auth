// Package bootstrap builds the application: its configuration and providers.
package bootstrap

import (
	"fullstack"
	database "fullstack-orm"
	"orm"
	postgres "orm-postgres"

	"luna-auth/app/providers"
	"luna-auth/database/migrations"
)

// NewApp creates the application with every provider registered. Tests pass
// Options to choose the .env files and the log output.
func NewApp(opts ...fullstack.Options) (*fullstack.App, error) {
	app, err := fullstack.New(opts...)
	if err != nil {
		return nil, err
	}
	err = app.Register(
		&database.Provider{
			Drivers:    map[string]func(string) orm.Driver{"postgres": postgres.Driver},
			Migrations: migrations.All,
		},
		&providers.KeysProvider{},
		&providers.RouteProvider{},
	)
	return app, err
}
