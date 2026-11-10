package tests

import (
	"fmt"
	"strings"
	"testing"

	apptest "fullstack-testing"
	"luna-keys"
)

// dataWorld is the shop with a login key, a data reader, a data writer, and
// the blog with a data writer of its own.
type dataWorld struct {
	c                                    *apptest.Client
	shop, reader, writer, blogWriter     string
	internal                             *apptest.Client // the dashboard's service token
	shopUser, shopAccess, blogUser, blog string
}

func newDataWorld(t *testing.T) *dataWorld {
	t.Helper()
	shop, shopHash, _ := keys.Generate(keys.Server)
	reader, readerHash, _ := keys.Generate(keys.Server)
	writer, writerHash, _ := keys.Generate(keys.Server)
	blog, blogHash, _ := keys.Generate(keys.Server)
	blogWriter, blogWriterHash, _ := keys.Generate(keys.Server)
	url := fakeDashboard(t, map[string]*keys.Key{
		shopHash:       {ID: "k1", AppID: shopID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthLogin}},
		readerHash:     {ID: "k2", AppID: shopID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthDataRead}},
		writerHash:     {ID: "k3", AppID: shopID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthDataRead, keys.AuthDataWrite, keys.AuthUsersWrite}},
		blogHash:       {ID: "k4", AppID: blogID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthLogin}},
		blogWriterHash: {ID: "k5", AppID: blogID, Kind: keys.Server, Scopes: []keys.Scope{keys.AuthDataRead, keys.AuthDataWrite}},
	})
	app, _ := newDBApp(t, url)
	c := apptest.New(t, app)
	w := &dataWorld{c: c, shop: shop, reader: reader, writer: writer, blogWriter: blogWriter, blog: blog,
		internal: c.WithToken(serviceToken)}
	// The same email in both applications: two different users.
	r := w.as(shop).PostJSON("/v1/users", map[string]any{"email": "ann@example.com", "password": "secret-password"}).AssertCreated()
	w.shopUser, w.shopAccess = str(t, r, "user.id"), str(t, r, "access_token")
	r = w.as(blog).PostJSON("/v1/users", map[string]any{"email": "ann@example.com", "password": "other-password"}).AssertCreated()
	w.blogUser = str(t, r, "user.id")
	return w
}

func (w *dataWorld) as(key string) *apptest.Client { return w.c.WithHeader(keys.Header, key) }

func TestUserData(t *testing.T) {
	w := newDataWorld(t)
	base := "/v1/users/" + w.shopUser + "/data"
	writer, reader := w.as(w.writer), w.as(w.reader)

	reader.GetJSON(base).AssertOK().AssertJSONPath("data", map[string]any{})
	w.as(w.shop).GetJSON(base).AssertForbidden()                                   // auth:login is not enough
	reader.PutJSON(base+"/plan", map[string]any{"value": "pro"}).AssertForbidden() // read only

	writer.PutJSON(base+"/plan", map[string]any{"value": "pro"}).AssertCreated().
		AssertJSONPath("key", "plan").AssertJSONPath("value", "pro")
	writer.PutJSON(base+"/plan", map[string]any{"value": "team"}).AssertOK().AssertJSONPath("value", "team")
	writer.PutJSON(base+"/note", map[string]any{"value": ""}).AssertCreated() // empty is a value
	writer.PutJSON(base+"/city", map[string]any{}).AssertUnprocessable()      // no value
	writer.PutJSON(base+"/bad%20key", map[string]any{"value": "x"}).AssertUnprocessable().AssertJSONValidationErrors("key")
	writer.PutJSON(base+"/big", map[string]any{"value": strings.Repeat("x", 16*1024+1)}).AssertUnprocessable().AssertJSONValidationErrors("value")

	reader.GetJSON(base+"/plan").AssertOK().AssertJSONPath("value", "team")
	reader.GetJSON(base + "/missing").AssertNotFound()
	var all struct{ Data map[string]string }
	reader.GetJSON(base).AssertOK().DecodeJSON(&all)
	if len(all.Data) != 2 || all.Data["plan"] != "team" || all.Data["note"] != "" {
		t.Fatalf("data: %v", all.Data)
	}

	// Several at once: set, replace, delete; all or nothing.
	all.Data = nil // json would merge into the old map
	writer.PatchJSON(base, map[string]any{"data": map[string]any{"profile.city": "Kyiv", "plan": "free", "note": nil}}).AssertOK().DecodeJSON(&all)
	if len(all.Data) != 2 || all.Data["profile.city"] != "Kyiv" || all.Data["plan"] != "free" {
		t.Fatalf("after patch: %v", all.Data)
	}
	writer.PatchJSON(base, map[string]any{"data": map[string]any{"ok": "1", "bad key": "2"}}).AssertUnprocessable().AssertJSONValidationErrors("data")
	reader.GetJSON(base + "/ok").AssertNotFound() // nothing of the refused patch was written

	writer.Delete(base + "/plan").AssertNoContent()
	writer.Delete(base + "/plan").AssertNotFound()

	// The user reads their own data with an access token; never writes it.
	shop := w.as(w.shop)
	shop.GetJSON("/v1/me/data").AssertUnauthorized()
	me := shop.WithToken(w.shopAccess)
	me.GetJSON("/v1/me/data").AssertOK().AssertJSONPath("data", map[string]any{"profile.city": "Kyiv"})
	me.GetJSON("/v1/me/data/plan").AssertNotFound()

	// Another application sees neither the user nor the data; its own user
	// with the same email has its own.
	blog := w.as(w.blogWriter)
	blog.GetJSON(base).AssertNotFound()
	blog.PutJSON(base+"/plan", map[string]any{"value": "x"}).AssertNotFound()
	blog.PutJSON("/v1/users/"+w.blogUser+"/data/plan", map[string]any{"value": "blogger"}).AssertCreated()
	reader.GetJSON(base + "/plan").AssertNotFound()

	// The data goes with the user.
	writer.Delete("/v1/users/" + w.shopUser).AssertNoContent()
	reader.GetJSON(base).AssertNotFound()
}

func TestUserDataLimit(t *testing.T) {
	w := newDataWorld(t)
	base := "/v1/users/" + w.shopUser + "/data"
	writer := w.as(w.writer)
	many := map[string]any{}
	for i := 0; i < 100; i++ {
		many[fmt.Sprintf("k%03d", i)] = "v"
	}
	writer.PatchJSON(base, map[string]any{"data": many}).AssertOK().AssertJSONCount("data", 100)
	writer.PutJSON(base+"/k000", map[string]any{"value": "changed"}).AssertOK() // replacing is fine
	writer.PutJSON(base+"/one-more", map[string]any{"value": "v"}).AssertUnprocessable().AssertJSONValidationErrors("key")
	writer.PatchJSON(base, map[string]any{"data": map[string]any{"k000": nil, "one-more": "v"}}).AssertOK() // one out, one in
}

func TestInternalAPI(t *testing.T) {
	w := newDataWorld(t)
	shopBase := "/internal/v1/apps/" + shopID

	w.c.GetJSON(shopBase + "/users").AssertUnauthorized()
	w.c.WithToken("wrong").GetJSON(shopBase + "/users").AssertUnauthorized()
	w.internal.GetJSON("/internal/v1/apps/not-a-uuid/users").AssertNotFound()

	in := w.internal
	in.GetJSON(shopBase+"/users").AssertOK().AssertJSONPath("total", 1).AssertJSONPath("data.0.id", w.shopUser)
	r := in.PostJSON(shopBase+"/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "secret-password", "email_verified": true}).
		AssertCreated().AssertJSONPath("email_verified", true)
	bob := str(t, r, "id")
	in.PostJSON(shopBase+"/users", map[string]any{"email": "bob@example.com", "password": "secret-password"}).AssertUnprocessable().AssertJSONValidationErrors("email")
	in.GetJSON(shopBase+"/users?q=BO").AssertOK().AssertJSONPath("total", 1).AssertJSONPath("data.0.id", bob)
	in.GetJSON(shopBase+"/users?q=100%25").AssertOK().AssertJSONPath("total", 0) // % is literal
	in.GetJSON(shopBase+"/users?q=example").AssertOK().AssertJSONPath("total", 2)
	w.as(w.shop).PostJSON("/v1/sessions", map[string]any{"login": "bob", "password": "secret-password"}).AssertOK()

	in.PatchJSON(shopBase+"/users/"+bob, map[string]any{"username": "robert"}).AssertOK().AssertJSONPath("username", "robert")
	in.PutJSON(shopBase+"/users/"+bob+"/data/plan", map[string]any{"value": "pro"}).AssertCreated()
	in.PatchJSON(shopBase+"/users/"+bob+"/data", map[string]any{"data": map[string]any{"city": "Lviv"}}).AssertOK().AssertJSONCount("data", 2)
	in.GetJSON(shopBase+"/users/"+bob+"/data/plan").AssertOK().AssertJSONPath("value", "pro")
	w.as(w.reader).GetJSON("/v1/users/"+bob+"/data/city").AssertOK().AssertJSONPath("value", "Lviv") // the same store
	in.Delete(shopBase + "/users/" + bob + "/data/city").AssertNoContent()

	// The path decides the application.
	in.GetJSON("/internal/v1/apps/" + blogID + "/users/" + bob).AssertNotFound()
	in.GetJSON("/internal/v1/apps/"+blogID+"/users").AssertOK().AssertJSONPath("total", 1).AssertJSONPath("data.0.id", w.blogUser)

	in.Delete(shopBase + "/users/" + bob).AssertNoContent()
	in.GetJSON(shopBase + "/users/" + bob).AssertNotFound()
}
