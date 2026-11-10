// Package routes registers the application's routes.
package routes

import (
	"fullstack"
	"fullstack/env"
	"fullstack/web"
	"luna-keys"

	"luna-auth/app/http/controllers"
	"luna-auth/docs"
)

// API registers the public API. Every /v1 route needs an API key from the
// dashboard (X-API-Key) with the scope shown; browsers get their preflight
// answered by keys.CORS. The end user's access token travels in
// Authorization: Bearer.
func API(app *fullstack.App, v keys.Verifier, serviceToken string) {
	app.Router.Get("/healthz", func(c *web.Ctx) error {
		return c.JSON(200, map[string]string{"status": "ok", "app": app.Config.Name})
	}).Name("healthz")

	// The OpenAPI document is public like the API; Swagger UI only with API_DOCS=true.
	ui := env.Bool("API_DOCS", false)
	docs.Register(app.Router, docs.OpenAPI, "docs", "/openapi.yaml", "/docs", "Luna Auth API", ui)
	docs.Register(app.Router, docs.Internal, "docs.internal", "/internal/openapi.yaml", "/internal/docs", "Luna Auth internal API", ui)

	api := controllers.NewAPI(app)
	login := keys.RequireKey(v, keys.AuthLogin)
	read := keys.RequireKey(v, keys.AuthUsersRead)
	write := keys.RequireKey(v, keys.AuthUsersWrite)
	dataRead := keys.RequireKey(v, keys.AuthDataRead)
	dataWrite := keys.RequireKey(v, keys.AuthDataWrite)

	app.Router.Group("/v1", func(r *web.Router) {
		r.Use(keys.CORS())
		// Ping tells a client which application its key belongs to.
		r.Get("/ping", func(c *web.Ctx) error {
			k, _ := keys.FromContext(c.Context())
			return c.JSON(200, map[string]any{"app_id": k.AppID, "app_name": k.AppName, "scopes": k.Scopes})
		}, login).Name("v1.ping")

		r.Post("/users", api.Register, login).Name("v1.register")
		r.Post("/sessions", api.Login, login).Name("v1.login")
		r.Post("/tokens/refresh", api.Refresh, login).Name("v1.refresh")
		r.Post("/tokens/revoke", api.Revoke, login).Name("v1.logout")
		r.Post("/tokens/verify", api.Verify, login).Name("v1.verify")
		r.Get("/me", api.Me, login).Name("v1.me")
		r.Get("/me/data", api.MyData, login).Name("v1.me.data")
		r.Get("/me/data/{key}", api.MyDataKey, login)
		r.Post("/password/forgot", api.Forgot, login).Name("v1.password.forgot")
		r.Post("/password/reset", api.Reset, login).Name("v1.password.reset")

		r.Get("/users", api.ListUsers, read).Name("v1.users")
		r.Get("/users/{id}", api.ShowUser, read).Name("v1.users.show")
		r.Patch("/users/{id}", api.UpdateUser, write)
		r.Delete("/users/{id}", api.DeleteUser, write)

		r.Get("/users/{id}/data", api.UserData, dataRead).Name("v1.users.data")
		r.Patch("/users/{id}/data", api.PatchUserData, dataWrite)
		r.Get("/users/{id}/data/{key}", api.UserDataKey, dataRead)
		r.Put("/users/{id}/data/{key}", api.PutUserData, dataWrite)
		r.Delete("/users/{id}/data/{key}", api.DeleteUserData, dataWrite)
	})

	// The dashboard's administration of an application's users. nginx does
	// not expose /internal/; the dashboard calls it over the service network.
	app.Router.Group("/internal/v1/apps/{app}", func(r *web.Router) {
		r.Get("/users", api.ListUsers).Name("internal.users")
		r.Post("/users", api.CreateUser)
		r.Get("/users/{id}", api.ShowUser)
		r.Patch("/users/{id}", api.UpdateUser)
		r.Delete("/users/{id}", api.DeleteUser)
		r.Get("/users/{id}/data", api.UserData)
		r.Patch("/users/{id}/data", api.PatchUserData)
		r.Get("/users/{id}/data/{key}", api.UserDataKey)
		r.Put("/users/{id}/data/{key}", api.PutUserData)
		r.Delete("/users/{id}/data/{key}", api.DeleteUserData)
	}, controllers.Internal(serviceToken))
}
