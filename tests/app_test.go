// Package tests holds the application's HTTP tests. The dashboard is faked
// with luna-keys' own introspection handler; routes that touch the
// database will need TEST_DATABASE_DSN (Postgres).
package tests

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"

	"fullstack"
	apptest "fullstack-testing"
	"luna-keys"

	"luna-auth/bootstrap"
)

const serviceToken = "auth-service-token"

// fakeDashboard knows the keys it is given and nothing else.
func fakeDashboard(t *testing.T, known map[string]*keys.Key) string {
	srv := httptest.NewServer(keys.Handler(func(_ context.Context, hash string) (*keys.Key, error) {
		return known[hash], nil
	}, serviceToken))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newApp(t *testing.T, dashboardURL string) *fullstack.App {
	t.Helper()
	for k, v := range map[string]string{
		"APP_NAME": "luna-auth", "APP_ENV": "testing",
		"DB_DRIVER": "postgres", "DATABASE_DSN": "postgres://unused@127.0.0.1:1/none", "DB_PING": "false", "DB_MIGRATE": "false",
		"LUNA_DASHBOARD_URL": dashboardURL, "LUNA_DASHBOARD_TOKEN": serviceToken,
	} {
		t.Setenv(k, v)
	}
	app, err := bootstrap.NewApp(fullstack.Options{EnvFiles: []string{}, LogOutput: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestHealth(t *testing.T) {
	apptest.New(t, newApp(t, fakeDashboard(t, nil))).GetJSON("/healthz").AssertOK().AssertJSONPath("status", "ok")
}

func TestPingNeedsAKey(t *testing.T) {
	server, serverHash, _ := keys.Generate(keys.Server)
	browser, browserHash, _ := keys.Generate(keys.Browser)
	pagesOnly, pagesHash, _ := keys.Generate(keys.Server)
	url := fakeDashboard(t, map[string]*keys.Key{
		serverHash:  {ID: "k1", AppID: "app-1", AppName: "Shop", Kind: keys.Server, Scopes: []keys.Scope{keys.AuthLogin}},
		browserHash: {ID: "k2", AppID: "app-1", Kind: keys.Browser, Scopes: []keys.Scope{keys.AuthLogin}, Origins: []string{"https://shop.example"}},
		pagesHash:   {ID: "k3", AppID: "app-1", Kind: keys.Server, Scopes: []keys.Scope{keys.PagesRead}},
	})
	c := apptest.New(t, newApp(t, url))

	c.GetJSON("/v1/ping").AssertUnauthorized()
	c.WithHeader(keys.Header, "sk_live_nope").GetJSON("/v1/ping").AssertUnauthorized()
	c.WithHeader(keys.Header, server).GetJSON("/v1/ping").AssertOK().
		AssertJSONPath("app_id", "app-1").AssertJSONPath("app_name", "Shop")
	c.WithHeader(keys.Header, pagesOnly).GetJSON("/v1/ping").AssertForbidden()

	fromShop := c.WithHeader(keys.Header, browser).WithHeader("Origin", "https://shop.example")
	fromShop.GetJSON("/v1/ping").AssertOK().AssertHeader("Access-Control-Allow-Origin", "https://shop.example")
	c.WithHeader(keys.Header, browser).WithHeader("Origin", "https://evil.example").GetJSON("/v1/ping").AssertForbidden()
	c.WithHeader(keys.Header, server).WithHeader("Origin", "https://shop.example").GetJSON("/v1/ping").AssertForbidden()
}

func TestDashboardDown(t *testing.T) {
	srv := httptest.NewServer(nil)
	srv.Close() // nothing listens there any more
	server, _, _ := keys.Generate(keys.Server)
	apptest.New(t, newApp(t, srv.URL)).WithHeader(keys.Header, server).GetJSON("/v1/ping").AssertStatus(503)
}
