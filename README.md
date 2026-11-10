# luna-auth

Registration and login of the end users of applications registered in luna-dashboard. Every
`/v1` request carries the application's API key in `X-API-Key` and is checked through
[luna-keys](../luna-keys) against the dashboard; the key decides the application and its scopes.

An API-only [fullstack](https://github.com/shibisty/fullstack.go) application on Postgres.

State: stage 2 of the migration plan — `GET /healthz` and `GET /v1/ping` (any key with
`auth:login`: which application the key belongs to). Users, sessions and tokens arrive in stage 5.

## Run

From the workspace root (`luna/`): `gtr install`. Then here:

```bash
cp .env.example .env     # DATABASE_DSN, LUNA_DASHBOARD_URL, LUNA_DASHBOARD_TOKEN
gtr dev                  # http://localhost:8081
```

| Variable | |
|---|---|
| `DATABASE_DSN` | Postgres, e.g. `postgres://auth_service:…@postgres:5432/auth_db` |
| `LUNA_DASHBOARD_URL` | the dashboard's internal address |
| `LUNA_DASHBOARD_TOKEN` | this service's token, one of the dashboard's `LUNA_SERVICE_TOKENS` |
| `LUNA_KEYS_CACHE_TTL` | how long a valid key is trusted without asking again (60 s) |
| `HTTP_TRUSTED_PROXIES` | nginx's network, so key network allow-lists see the client |

```bash
curl -H "X-API-Key: sk_live_…" http://localhost:8081/v1/ping
```

## Tests

`gtr run test` — the dashboard is faked with luna-keys' own handler.

## License

MIT
