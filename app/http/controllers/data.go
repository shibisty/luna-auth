package controllers

import (
	"errors"
	"net/http"

	"fullstack"
	"fullstack-validation"
	"fullstack/web"

	"luna-auth/app/models"
	"luna-auth/app/userdata"
)

// ---- the application's data about its users ----
//
// String values by key, per user (app/userdata). Servers read them with
// auth:data.read and write them with auth:data.write; the user reads
// their own with an access token (/v1/me/data).

func (h API) data() (*userdata.Service, error) { return fullstack.Make[*userdata.Service](h.App) }

func dataError(err error) error {
	if errors.Is(err, userdata.ErrNotFound) {
		return web.Error(http.StatusNotFound, "The user has no value under this key.")
	}
	return userError(err)
}

// dataJSON is every value of a user, as one object.
func dataJSON(entries []userdata.Entry) map[string]any {
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Key] = e.Value
	}
	return map[string]any{"data": m}
}

func (h API) allData(c *web.Ctx, u *models.User) error {
	svc, err := h.data()
	if err != nil {
		return err
	}
	entries, err := svc.All(c.Context(), u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dataJSON(entries))
}

func (h API) oneData(c *web.Ctx, u *models.User) error {
	svc, err := h.data()
	if err != nil {
		return err
	}
	e, err := svc.Get(c.Context(), u, c.Param("key"))
	if err != nil {
		return dataError(err)
	}
	return c.JSON(http.StatusOK, e)
}

// UserData is GET /v1/users/{id}/data: {"data": {key: value}}.
func (h API) UserData(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	return h.allData(c, u)
}

// UserDataKey is GET /v1/users/{id}/data/{key}.
func (h API) UserDataKey(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	return h.oneData(c, u)
}

type putDataIn struct {
	Value *string `json:"value"` // required, may be "" (checked by hand: "required" refuses "")
}

// PutUserData is PUT /v1/users/{id}/data/{key} {"value": "…"}: 201 for a
// new key, 200 for a changed one.
func (h API) PutUserData(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	var in putDataIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	if in.Value == nil {
		return invalid("value", "The value field is required.")
	}
	svc, err := h.data()
	if err != nil {
		return err
	}
	e, created, err := svc.Set(c.Context(), u, c.Param("key"), *in.Value)
	if err != nil {
		return dataError(err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return c.JSON(status, e)
}

type patchDataIn struct {
	Data map[string]*string `json:"data" validate:"required"`
}

// PatchUserData is PATCH /v1/users/{id}/data {"data": {key: value|null}}:
// sets the keys given a string, deletes the keys given null, all or
// nothing; answers with every value afterwards.
func (h API) PatchUserData(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	var in patchDataIn
	if err := validation.Bind(c, &in); err != nil {
		return err
	}
	if len(in.Data) > userdata.MaxKeys {
		return invalid("data", "Too many keys in one request.")
	}
	svc, err := h.data()
	if err != nil {
		return err
	}
	entries, err := svc.Patch(c.Context(), u, in.Data)
	if err != nil {
		return dataError(err)
	}
	return c.JSON(http.StatusOK, dataJSON(entries))
}

// DeleteUserData is DELETE /v1/users/{id}/data/{key}.
func (h API) DeleteUserData(c *web.Ctx) error {
	_, u, err := h.user(c)
	if err != nil {
		return err
	}
	svc, err := h.data()
	if err != nil {
		return err
	}
	if err := svc.Delete(c.Context(), u, c.Param("key")); err != nil {
		return dataError(err)
	}
	return c.NoContent()
}

// MyData is GET /v1/me/data: the access token's user reads their own values.
func (h API) MyData(c *web.Ctx) error {
	u, err := h.bearer(c)
	if err != nil {
		return err
	}
	return h.allData(c, u)
}

// MyDataKey is GET /v1/me/data/{key}.
func (h API) MyDataKey(c *web.Ctx) error {
	u, err := h.bearer(c)
	if err != nil {
		return err
	}
	return h.oneData(c, u)
}
