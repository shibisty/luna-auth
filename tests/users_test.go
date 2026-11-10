package tests

import (
	"regexp"
	"strings"
	"testing"

	apptest "fullstack-testing"
	"luna-keys"
)

const (
	shopID = "11111111-1111-4111-8111-111111111111"
	blogID = "22222222-2222-4222-8222-222222222222"
)

// world is two applications with their keys.
type world struct {
	c                                    *apptest.Client
	box                                  *outbox
	shop, shopAdmin, shopReader, blogKey string // keys
}

func newWorld(t *testing.T) *world {
	t.Helper()
	shop, shopHash, _ := keys.Generate(keys.Server)
	admin, adminHash, _ := keys.Generate(keys.Server)
	reader, readerHash, _ := keys.Generate(keys.Server)
	blog, blogHash, _ := keys.Generate(keys.Server)
	url := fakeDashboard(t, map[string]*keys.Key{
		shopHash:   {ID: "k1", AppID: shopID, AppName: "Shop", Kind: keys.Server, Scopes: []keys.Scope{keys.AuthLogin}, PasswordResetURL: "https://shop.example/reset?lang=en"},
		adminHash:  {ID: "k2", AppID: shopID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthUsersRead, keys.AuthUsersWrite}},
		readerHash: {ID: "k3", AppID: shopID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthUsersRead}},
		blogHash:   {ID: "k4", AppID: blogID, AppName: "Blog", Kind: keys.Server, Scopes: []keys.Scope{keys.AuthLogin}},
	})
	app, box := newDBApp(t, url)
	return &world{c: apptest.New(t, app), box: box, shop: shop, shopAdmin: admin, shopReader: reader, blogKey: blog}
}

func (w *world) as(key string) *apptest.Client { return w.c.WithHeader(keys.Header, key) }

func str(t *testing.T, r *apptest.Response, path string) string {
	t.Helper()
	v, ok := r.JSONPath(path)
	s, _ := v.(string)
	if !ok || s == "" {
		t.Fatalf("no %s in %s", path, r.Text())
	}
	return s
}

func TestRegisterLoginAndTokens(t *testing.T) {
	w := newWorld(t)
	shop := w.as(w.shop)

	r := shop.PostJSON("/v1/users", map[string]any{"username": "Ann", "email": "Ann@Example.com", "password": "secret-password"}).
		AssertCreated().AssertJSONPath("user.email", "ann@example.com").AssertJSONPath("user.username", "ann").
		AssertJSONPath("token_type", "Bearer").AssertHeader("Cache-Control", "no-store")
	userID := str(t, r, "user.id")
	access := str(t, r, "access_token")
	str(t, r, "refresh_token")

	// validation and uniqueness within the application
	shop.PostJSON("/v1/users", map[string]any{"email": "ann@example.com", "password": "secret-password"}).
		AssertUnprocessable().AssertJSONValidationErrors("email")
	shop.PostJSON("/v1/users", map[string]any{"username": "ANN", "email": "other@example.com", "password": "secret-password"}).
		AssertUnprocessable().AssertJSONValidationErrors("username")
	shop.PostJSON("/v1/users", map[string]any{"username": "a@b", "email": "x@example.com", "password": "secret-password"}).
		AssertUnprocessable().AssertJSONValidationErrors("username")
	shop.PostJSON("/v1/users", map[string]any{"email": "not-an-email", "password": "short"}).
		AssertUnprocessable().AssertJSONValidationErrors("email", "password")
	// another application has its own users: the same email is free there
	w.as(w.blogKey).PostJSON("/v1/users", map[string]any{"email": "ann@example.com", "password": "another-password"}).AssertCreated()

	// login by email or username, case-insensitive
	shop.PostJSON("/v1/sessions", map[string]any{"login": "ANN@example.com", "password": "secret-password"}).AssertOK()
	r = shop.PostJSON("/v1/sessions", map[string]any{"login": "ann", "password": "secret-password"}).AssertOK().
		AssertJSONPath("user.id", userID)
	refresh1 := str(t, r, "refresh_token")
	shop.PostJSON("/v1/sessions", map[string]any{"login": "ann", "password": "wrong-password"}).AssertUnauthorized()
	shop.PostJSON("/v1/sessions", map[string]any{"login": "nobody", "password": "secret-password"}).AssertUnauthorized()
	w.as(w.blogKey).PostJSON("/v1/sessions", map[string]any{"login": "ann@example.com", "password": "secret-password"}).AssertUnauthorized()

	// the access token
	shop.WithToken(access).GetJSON("/v1/me").AssertOK().AssertJSONPath("id", userID).AssertJSONPath("email", "ann@example.com")
	shop.GetJSON("/v1/me").AssertUnauthorized()
	shop.WithToken("garbage").GetJSON("/v1/me").AssertUnauthorized()
	w.as(w.blogKey).WithToken(access).GetJSON("/v1/me").AssertUnauthorized() // aud is the shop
	shop.PostJSON("/v1/tokens/verify", map[string]any{"token": access}).AssertOK().
		AssertJSONPath("active", true).AssertJSONPath("sub", userID).AssertJSONPath("user.username", "ann")
	w.as(w.blogKey).PostJSON("/v1/tokens/verify", map[string]any{"token": access}).AssertOK().AssertJSONPath("active", false)

	// refresh rotates; a reused refresh token ends the login
	r = shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": refresh1}).AssertOK()
	refresh2 := str(t, r, "refresh_token")
	shop.WithToken(str(t, r, "access_token")).GetJSON("/v1/me").AssertOK()
	shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": refresh1}).AssertUnauthorized()
	shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": refresh2}).AssertUnauthorized() // family revoked

	// a refresh token works only with its own application's key
	r = shop.PostJSON("/v1/sessions", map[string]any{"login": "ann", "password": "secret-password"}).AssertOK()
	w.as(w.blogKey).PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": str(t, r, "refresh_token")}).AssertUnauthorized()

	// logout
	r = shop.PostJSON("/v1/sessions", map[string]any{"login": "ann", "password": "secret-password"}).AssertOK()
	refresh3 := str(t, r, "refresh_token")
	shop.PostJSON("/v1/tokens/revoke", map[string]any{"refresh_token": refresh3}).AssertNoContent()
	shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": refresh3}).AssertUnauthorized()
	shop.PostJSON("/v1/tokens/revoke", map[string]any{"refresh_token": "unknown"}).AssertNoContent()
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]{43})`)

func TestPasswordReset(t *testing.T) {
	w := newWorld(t)
	shop := w.as(w.shop)
	shop.PostJSON("/v1/users", map[string]any{"email": "bo@example.com", "password": "old-password"}).AssertCreated()
	r := shop.PostJSON("/v1/sessions", map[string]any{"login": "bo@example.com", "password": "old-password"}).AssertOK()
	oldRefresh := str(t, r, "refresh_token")

	// unknown emails look the same and send nothing
	shop.PostJSON("/v1/password/forgot", map[string]any{"email": "nobody@example.com"}).AssertStatus(202)
	if _, ok := w.box.last(); ok {
		t.Fatal("mail sent for an unknown email")
	}
	// an application without a reset URL cannot send links
	w.as(w.blogKey).PostJSON("/v1/password/forgot", map[string]any{"email": "bo@example.com"}).AssertStatus(409)

	shop.PostJSON("/v1/password/forgot", map[string]any{"email": "BO@example.com"}).AssertStatus(202)
	m, ok := w.box.last()
	if !ok || m.To != "bo@example.com" || !strings.Contains(m.Subject, "Shop") {
		t.Fatalf("mail: %+v", m)
	}
	match := tokenRe.FindStringSubmatch(m.Body)
	if match == nil || !strings.Contains(m.Body, "https://shop.example/reset?lang=en&token=") {
		t.Fatalf("no reset link in %q", m.Body)
	}
	token := match[1]

	shop.PostJSON("/v1/password/reset", map[string]any{"token": token, "password": "short"}).AssertUnprocessable()
	w.as(w.blogKey).PostJSON("/v1/password/reset", map[string]any{"token": token, "password": "new-password"}).
		AssertUnprocessable().AssertJSONValidationErrors("token")
	shop.PostJSON("/v1/password/reset", map[string]any{"token": token, "password": "new-password"}).AssertNoContent()
	shop.PostJSON("/v1/password/reset", map[string]any{"token": token, "password": "newer-password"}).AssertUnprocessable()

	shop.PostJSON("/v1/sessions", map[string]any{"login": "bo@example.com", "password": "old-password"}).AssertUnauthorized()
	shop.PostJSON("/v1/sessions", map[string]any{"login": "bo@example.com", "password": "new-password"}).AssertOK()
	shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": oldRefresh}).AssertUnauthorized() // logins ended
}

func TestAdministration(t *testing.T) {
	w := newWorld(t)
	shop := w.as(w.shop)
	r := shop.PostJSON("/v1/users", map[string]any{"username": "cyd", "email": "cy@example.com", "password": "secret-password"}).AssertCreated()
	id := str(t, r, "user.id")
	access := str(t, r, "access_token")
	refreshTok := str(t, r, "refresh_token")
	w.as(w.blogKey).PostJSON("/v1/users", map[string]any{"email": "dee@example.com", "password": "secret-password"}).AssertCreated()

	shop.GetJSON("/v1/users").AssertForbidden() // auth:login is not enough
	reader := w.as(w.shopReader)
	reader.GetJSON("/v1/users").AssertOK().AssertJSONCount("data", 1).AssertJSONPath("data.0.id", id).AssertJSONPath("total", 1)
	reader.GetJSON("/v1/users/"+id).AssertOK().AssertJSONPath("username", "cyd")
	reader.GetJSON("/v1/users/33333333-3333-4333-8333-333333333333").AssertNotFound()
	reader.PatchJSON("/v1/users/"+id, map[string]any{"email_verified": true}).AssertForbidden()

	admin := w.as(w.shopAdmin)
	admin.PatchJSON("/v1/users/"+id, map[string]any{"email_verified": true, "username": "cyrus"}).AssertOK().
		AssertJSONPath("email_verified", true).AssertJSONPath("username", "cyrus")
	admin.PatchJSON("/v1/users/"+id, map[string]any{"username": "no spaces"}).AssertUnprocessable()
	admin.PatchJSON("/v1/users/"+id, map[string]any{"email": "cy2@example.com"}).AssertOK().
		AssertJSONPath("email", "cy2@example.com").AssertJSONPath("email_verified", false)
	admin.PatchJSON("/v1/users/"+id, map[string]any{"password": "admin-set-password"}).AssertOK()
	shop.PostJSON("/v1/tokens/refresh", map[string]any{"refresh_token": refreshTok}).AssertUnauthorized() // a new password ends logins
	shop.PostJSON("/v1/sessions", map[string]any{"login": "cyrus", "password": "admin-set-password"}).AssertOK()

	admin.Delete("/v1/users/" + id).AssertNoContent()
	reader.GetJSON("/v1/users/" + id).AssertNotFound()
	shop.WithToken(access).GetJSON("/v1/me").AssertUnauthorized()
}

func TestLoginThrottle(t *testing.T) {
	w := newWorld(t)
	shop := w.as(w.shop)
	for i := 0; i < 10; i++ {
		shop.PostJSON("/v1/sessions", map[string]any{"login": "x@example.com", "password": "wrong-password"}).AssertUnauthorized()
	}
	r := shop.PostJSON("/v1/sessions", map[string]any{"login": "x@example.com", "password": "wrong-password"}).AssertStatus(429)
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// another application is counted apart
	w.as(w.blogKey).PostJSON("/v1/sessions", map[string]any{"login": "x@example.com", "password": "wrong-password"}).AssertUnauthorized()
}
