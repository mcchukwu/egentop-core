# Egentop-Core

Egentop-Core is the core backend service for the Egentop project. It is a
multi-tenant REST API for managing organizations, projects, milestones,
assignments and members with JWT authentication and per-organization role-based
access control (RBAC).

## Features

- Modular clean architecture (handler → service → repository)
- REST API with `/v1` versioning
- JWT access tokens + rotating HttpOnly refresh-token cookies
- Per-organization RBAC with system template roles (`owner`, `admin`, `member`, `viewer`)
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
git clone https://github.com/mcchukwu/egentop-core.git ./egentop
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

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_NAME` | `Egentop` | Application name |
| `APP_PORT` | `8080` | HTTP listen port |
| `APP_ENV` | `development` | `development` or `production` |
| `DB_URL` | — | PostgreSQL connection URL |
| `LOG_LEVEL` | `debug` | Log level |
| `JWT_SECRET` | — | JWT signing secret, **at least 32 characters** |
| `JWT_ACCESS_TTL` | `15m` | Access token lifetime (Go duration) |
| `JWT_REFRESH_TTL` | `720h` | Refresh token lifetime (Go duration) |
| `REQUIRE_EMAIL_VERIFICATION` | `false` | Require verified email before password change |
| `CORS_ALLOWED_ORIGINS` | — | Comma-separated allowed origins (required) |

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
