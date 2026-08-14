# Egentop-Core

Egentop is an AI-powered operations platform for service businesses, built in
four layers: **Workflow → Operations → Financial → Intelligence**. This
repository is the core backend — a multi-tenant REST API with JWT
authentication, per-organization role-based access control (RBAC), and a
workflow layer that already covers the near-term product wedge: clients,
milestone approvals, revision limits, deliverables and payment status.

**Current phase: API-first validation.** The backend (MVP + Layer-1 delta +
reliability pass) is complete; there is no frontend yet. See the
[API Reference](docs/api.md) for what the API does, the
[Roadmap](docs/roadmap.md) for where the product is going, and
[`.captain/ROADMAP.md`](.captain/ROADMAP.md) for the canonical roadmap.

## Features

- Modular clean architecture (handler → service → repository)
- REST API with `/v1` versioning
- JWT access tokens + rotating HttpOnly refresh-token cookies
- Per-organization RBAC with system template roles (`owner`, `admin`, `member`, `viewer`, `client`)
- Multi-tenant data isolation enforced at the query level
- PostgreSQL persistence with `update_updated_at_column()` triggers
- Audit logging and a project activity feed
- Paginated list endpoints
- Centralized error handling and structured logging
- Input validation with `go-playground/validator/v10`
- Rate limiting on auth endpoints

## Tech Stack

| Layer | Technology |
|--------|------------|
| Language | Go 1.26+ |
| Database | PostgreSQL |
| Authentication | JWT + HttpOnly refresh cookie |
| Validation | go-playground/validator/v10 |
| Containerization | Docker & Docker Compose |
| Testing | Go testing package |

## Architecture

```mermaid
flowchart TD
    Client --> API
    API --> Service
    Service --> Repository
    Repository --> PostgreSQL
```

## Quick Start

### Prerequisites

- Go 1.26+
- Docker & Docker Compose
- Git

### Clone

```bash
git clone https://github.com/mcchukwu/egentop ./egentop
cd egentop
```

### Configure

```bash
cp .env.example .env
```

Edit `.env` and set a `JWT_SECRET` of at least 32 characters, the database
credentials, and at least one `CORS_ALLOWED_ORIGINS` value.

### Start Dependencies

```bash
docker compose up -d
```

### Apply Migrations

With the `migrate` CLI installed:

```bash
make migrate-up
```

This applies `migrations/*.up.sql` in order to the database in your `.env`.

Alternatively, pipe the SQL into the container (adjust `-U`/`-d` to your
`DB_USER`/`DB_NAME`):

```bash
cat migrations/*.up.sql | docker compose exec -T postgres psql -U miracle -d egentop_db
```

### Run

```bash
go run ./cmd/api
```

The server listens on `:8080` by default (or `APP_PORT`).

## Environment Variables

The application reads exactly these variables (see `pkg/config/config.go`):

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `8080` | HTTP listen port |
| `APP_ENV` | — | Required: `development` or `production` (`production` makes the refresh cookie `Secure`) |
| `DB_URL` | — | PostgreSQL connection URL (required) |
| `JWT_SECRET` | — | JWT signing secret, **at least 32 characters** (required) |
| `JWT_ACCESS_TTL` | `15m` | Access token lifetime (Go duration) |
| `JWT_REFRESH_TTL` | `720h` | Refresh token lifetime (Go duration) |
| `CORS_ALLOWED_ORIGINS` | — | Comma-separated allowed origins (required) |
| `LOG_LEVEL` | `info` | One of `debug`, `info`, `warn`, `error` (unknown → `info`) |

The `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` /
`DB_SSLMODE` variables in `.env.example` are used by the `Makefile` targets
(`migrate-up`, `migrate-down`, `authz-decisions-cleanup`) to build a local DSN
— the running app reads `DB_URL` only.

## Project Structure

```text
cmd/           Application entry points.
internal/      Private application code.
pkg/           Reusable packages.
migrations/    SQL migrations (apply in numeric order).
docs/          Technical documentation.
```

## Documentation

- [API Reference](docs/api.md)
- [Architecture](docs/architecture.md)
- [Development Setup](docs/development-setup.md)
- [Deployment Guide](docs/deployment.md)
- [Security Practices](docs/security.md)
- [Coding Standards](docs/coding-standards.md)
- [Roadmap](docs/roadmap.md)

## Development Workflow

1. Create a feature branch.
2. Implement changes.
3. Add or update tests.
4. Run `go build ./...`, `go vet ./...` and `go test ./...`.
5. Open a pull request.

Integration tests require a running PostgreSQL and the `EGTEST_DB_URL`
environment variable, e.g.:

```bash
EGTEST_DB_URL='postgres://user:password@localhost:5432/egentop_test' go test ./...
```

## License

This project is licensed under the MIT License. See the LICENSE file for details.
