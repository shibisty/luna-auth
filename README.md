# Auth Service

This service provides authentication endpoints for the Luna project. It is implemented in Go with Gin and uses PostgreSQL as the backing store.

## Features

- User registration
- User login
- JWT-based token verification
- PostgreSQL connectivity with optional TLS settings
- Automatic table creation on startup

## Technology stack

- Go
- Gin
- PostgreSQL via pgx
- JWT
- bcrypt

## Requirements

- Go 1.22+
- PostgreSQL
- Environment variables for database and secret configuration

## Environment variables

The service reads the following variables:

- `SECRET_KEY` — JWT signing secret (default: `super-secret`)
- `DB_HOST` — PostgreSQL host (default: `localhost`)
- `DB_PORT` — PostgreSQL port (default: `5432`)
- `DB_NAME` — database name (default: `auth_db`)
- `DB_USER` — database user (default: `postgres`)
- `DB_PASSWORD` — database password
- `DB_SSLMODE` — SSL mode (default: `disable`)
- `DB_SSLROOTCERT`, `DB_SSLCERT`, `DB_SSLKEY`, `DB_SSLPASSWORD` — optional TLS settings
- `DB_PREFIX` — optional table prefix

## Run locally

From the auth directory:

```bash
go mod tidy
go run .
```

The service starts on port `8080`.

## API endpoints

- `GET /api/v1/ping` — health check
- `POST /api/v1/register` — create a new user
- `POST /api/v1/login` — authenticate a user and return a JWT
- `GET /api/v1/verify?token=...` — validate a token

## Docker

Build and run the service with the project stack:

```bash
docker compose up --build
```

The auth service will be available at:

- `http://localhost:8000`

## Kubernetes

The project includes Kubernetes manifests for deploying the auth service and PostgreSQL. Apply them with:

```bash
kubectl apply -f kubernetes.yml
```

If you are using Minikube, build the image first:

```bash
minikube image build -t auth:latest ./auth
```
