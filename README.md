# luna-auth

Registration and login of the end users of applications registered in luna-dashboard. Every
`/v1` request carries the application's API key in `X-API-Key` and is checked through
[luna-keys](../luna-keys) against the dashboard; the key decides the application and its scopes.
Users belong to one application: the same email can register in two applications, and a token
of one is refused by the other. Each application also keeps its own data about its users: string
values by key (`user_data`), which go with the user.

An API-only [fullstack](https://github.com/shibisty/fullstack.go) application on Postgres.

## API

| Request | Scope | |
|---|---|---|
| `POST /v1/users` `{username?, email, password}` | `auth:login` | register; answers like a login (201) |
| `POST /v1/sessions` `{login, password}` | `auth:login` | log in by username or email |
| `POST /v1/tokens/refresh` `{refresh_token}` | `auth:login` | a new pair; the old refresh token is spent, reusing it ends that login |
| `POST /v1/tokens/revoke` `{refresh_token}` | `auth:login` | log out (204) |
| `GET /v1/me` + `Authorization: Bearer <access>` | `auth:login` | the user of the access token |
| `POST /v1/tokens/verify` `{token}` | `auth:login` | for a client's backend: `{active, sub, exp, user}` |
| `POST /v1/password/forgot` `{email}` | `auth:login` | emails a link to the app's reset URL with `?token=`; 202 either way |
| `POST /v1/password/reset` `{token, password}` | `auth:login` | 204; every login of the user ends |
| `GET /v1/users?q=&page=&per_page=`, `GET /v1/users/{id}` | `auth:users.read` | administration (server keys); `q` searches emails and usernames |
| `PATCH /v1/users/{id}` `{username?, email?, email_verified?, password?}`, `DELETE /v1/users/{id}` | `auth:users.write` | a new password or a deletion ends the user's logins |
| `GET /v1/users/{id}/data`, `GET /v1/users/{id}/data/{key}` | `auth:data.read` | the application's data about the user: `{"data": {key: value}}`, one `{key, value, created_at, updated_at}` |
| `PUT /v1/users/{id}/data/{key}` `{value}`, `DELETE /v1/users/{id}/data/{key}` | `auth:data.write` | set (201 new key, 200 changed), delete (204) |
| `PATCH /v1/users/{id}/data` `{data: {key: value\|null}}` | `auth:data.write` | set several and delete the `null` ones, all or nothing |
| `GET /v1/me/data`, `GET /v1/me/data/{key}` + `Authorization: Bearer <access>` | `auth:login` | the user reads their own data (read-only) |
| `GET /v1/ping`, `GET /healthz` | `auth:login`, none | which application a key belongs to; liveness |

A login answers `{user, access_token, refresh_token, token_type: "Bearer", expires_in}` with
`Cache-Control: no-store`. The access token is an HS256 JWT (`iss=luna-auth`, `aud=<app id>`,
`sub=<user id>`, 15 minutes); the refresh token is random, stored as its SHA-256
(`passport_refresh_tokens`), 30 days, rotated on every use. Usernames are 3–32 of `a-z 0-9 . _ -`
and never contain `@`, so a login with `@` is an email. Emails and usernames are compared
lower-case. Registration, login and reset requests are limited to 10 a minute per application
and client address (429 with `Retry-After`). Errors are `{"error": ...}`, validation errors 422
with `errors` by field.

Data keys are 1–64 of `A-Z a-z 0-9 _ . : -`, starting with a letter or a digit (`profile.city`);
values are UTF-8 text up to 16 KiB (`""` is a value); a user has at most 100 keys.

The dashboard manages an application's users and their data through the **internal API**,
`/internal/v1/apps/{app}/users…` (list with `q`, create without a login, get, change, delete;
the same data operations), with the service token `LUNA_DASHBOARD_TOKEN` in
`Authorization: Bearer`. The application comes from the path, so the dashboard checks the
developer's ownership first; nginx does not route `/internal/`.

```bash
curl -H "X-API-Key: sk_live_…" -H "Content-Type: application/json" \
     -d '{"login":"ann","password":"…"}' http://localhost:8081/v1/sessions
```

## Documentation

- **OpenAPI 3.1**: [`docs/openapi.yaml`](docs/openapi.yaml), served at `GET /openapi.yaml`; Swagger UI
  at `/docs` with `API_DOCS=true` (its scripts come from the jsDelivr CDN). A test keeps the
  document and the routes in step. The internal API: [`docs/internal.yaml`](docs/internal.yaml), at
  `/internal/openapi.yaml` and `/internal/docs`.
- **Postman**: import [`docs/postman/luna-auth.postman_collection.json`](docs/postman/luna-auth.postman_collection.json)
  and the environment [`docs/postman/luna-local.postman_environment.json`](docs/postman/luna-local.postman_environment.json);
  set `apiKey` (a server key with `auth:login`) and `adminKey` (a server key with
  `auth:users.read`, `auth:users.write`, `auth:data.read` and `auth:data.write`) from the dashboard. Run the folders in order: each
  login saves `accessToken` and `refreshToken` for the next requests; every run registers a new
  user and deletes it at the end. From the command line:

  ```bash
  npx newman run docs/postman/luna-auth.postman_collection.json \
      -e docs/postman/luna-local.postman_environment.json \
      --env-var apiKey=sk_live_… --env-var adminKey=sk_live_…
  ```

## Run

From the workspace root (`luna/`): `gtr install`. Then here:

```bash
cp .env.example .env     # DATABASE_DSN, AUTH_JWT_SECRET, LUNA_DASHBOARD_URL, LUNA_DASHBOARD_TOKEN
gtr dev                  # http://localhost:8081
```

| Variable | |
|---|---|
| `DATABASE_DSN` | Postgres, e.g. `postgres://auth_service:…@127.0.0.1:5433/auth_db?sslmode=require` |
| `AUTH_JWT_SECRET` | 32+ bytes; the service does not start without it |
| `AUTH_TOKEN_TTL`, `AUTH_REFRESH_TTL` | `15m`, `720h` |
| `LUNA_DASHBOARD_URL`, `LUNA_DASHBOARD_TOKEN` | the dashboard's internal address and this service's token (one of its `LUNA_SERVICE_TOKENS`) |
| `LUNA_KEYS_CACHE_TTL` | how long a valid key is trusted without asking again (60 s) |
| `MAIL_HOST`, `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_FROM` | SMTP for reset emails; without `MAIL_HOST` they are logged (local only) |
| `HTTP_TRUSTED_PROXIES` | nginx's network, so limits and key network allow-lists see the client |

## Tests

```bash
TEST_DATABASE_DSN=postgres://…/auth_test gtr run test
```

The dashboard is faked with luna-keys' own handler and mail with an outbox. Without
`TEST_DATABASE_DSN` the tests that need users are skipped.

## License

MIT
