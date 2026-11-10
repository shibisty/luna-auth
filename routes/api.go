// Package routes registers the application's routes.
package routes

import (
	"fullstack"
	"fullstack/web"
	"luna-keys"
)

// API registers the public API. Every /v1 route needs an API key from the
// dashboard (X-API-Key); browsers get their preflight answered by
// keys.CORS.
func API(app *fullstack.App, v keys.Verifier) {
	app.Router.Get("/healthz", func(c *web.Ctx) error {
		return c.JSON(200, map[string]string{"status": "ok", "app": app.Config.Name})
	}).Name("healthz")

	app.Router.Group("/v1", func(r *web.Router) {
		r.Use(keys.CORS())
		// Ping tells a client which application its key belongs to: a
		// quick check of the integration.
		r.Get("/ping", func(c *web.Ctx) error {
			k, _ := keys.FromContext(c.Context())
			return c.JSON(200, map[string]any{"app_id": k.AppID, "app_name": k.AppName, "scopes": k.Scopes})
		}, keys.RequireKey(v, keys.AuthLogin)).Name("v1.ping")
	})
}
