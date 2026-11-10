# luna-auth

Registration and login of the end users of applications registered in luna-dashboard. Every
`/v1` request carries the application's API key in `X-API-Key` and is checked through
[luna-keys](../luna-keys) against the dashboard; the key decides the application and its scopes.
Users belong to one application: the same email can register in two applications, and a token
of one is refused by the other.

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
| `GET /v1/users?page=&per_page=`, `GET /v1/users/{id}` | `auth:users.read` | administration (server keys) |
| `PATCH /v1/users/{id}` `{username?, email?, email_verified?, password?}`, `DELETE /v1/users/{id}` | `auth:users.write` | a new password or a deletion ends the user's logins |
| `GET /v1/ping`, `GET /healthz` | `auth:login`, none | which application a key belongs to; liveness |

A login answers `{user, access_token, refresh_token, token_type: "Bearer", expires_in}` with
`Cache-Control: no-store`. The access token is an HS256 JWT (`iss=luna-auth`, `aud=<app id>`,
`sub=<user id>`, 15 minutes); the refresh token is random, stored as its SHA-256
(`passport_refresh_tokens`), 30 days, rotated on every use. Usernames are 3–32 of `a-z 0-9 . _ -`
and never contain `@`, so a login with `@` is an email. Emails and usernames are compared
lower-case. Registration, login and reset requests are limited to 10 a minute per application
and client address (429 with `Retry-After`). Errors are `{"error": ...}`, validation errors 422
with `errors` by field.

```bash
curl -H "X-API-Key: sk_live_…" -H "Content-Type: application/json" \
     -d '{"login":"ann","password":"…"}' http://localhost:8081/v1/sessions
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
