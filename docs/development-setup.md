# Development Setup

## Prerequisites

- Go 1.26+
- Docker & Docker Compose (for the local PostgreSQL)
- Optional: `psql` client for inspecting the database

## One-Time Setup

### 1. Clone and install

```bash
git clone https://github.com/mcchukwu/egentop-core.git ./egentop
cd egentop
go mod download
```

### 2. Configure environment

```bash
cp .env.example .env
```

Edit `.env`:

- Set `JWT_SECRET` to a random string of at least 32 characters.
- Adjust database credentials and ports if needed.
- `CORS_ALLOWED_ORIGINS` must contain at least one origin.

### 3. Start PostgreSQL

```bash
docker compose up -d
```

This starts PostgreSQL (see `compose.yaml`, `postgres:18`) exposed on `DB_PORT`
(default `5432`) with a named volume so data survives container restarts.

### 4. Apply migrations

```bash
make migrate-up
```

This uses the `migrate` CLI to apply `migrations/*.up.sql` in numeric order
(`000001`, `000002`, `000003`, `000004`, `000005`) to the database in your
`.env`.

To roll back, run `make migrate-down` (one step at a time).

### 5. Run the server

```bash
go run ./cmd/api
```

The API listens on `APP_PORT` (default `8080`). Verify with:

```bash
curl http://localhost:8080/v1/health
```

## Running Tests

Unit tests and pure Go tests:

```bash
go test ./...
```

Integration tests (`*_integration_test.go`) need a real PostgreSQL. Point them
at a test database with `EGTEST_DB_URL`:

```bash
export EGTEST_DB_URL='postgres://user:password@localhost:5432/egentop_test'
go test -count=1 ./...
```

The integration tests expect a schema that matches the migrations. Create the
test database and apply the up migrations first:

```bash
docker compose exec -T postgres psql -U miracle -d egentop_db \
  -c 'CREATE DATABASE egentop_test;'
cat migrations/*.up.sql | docker compose exec -T postgres psql -U miracle -d egentop_test
```

Adjust `-U`/`-d` to your `.env` `DB_USER`/`DB_NAME`.

### Resetting the test database

Many integration tests truncate tables between runs. Because `memberships`
references `roles`, truncating `organizations CASCADE` also wipes the system
roles. If you see missing-role failures, reset the whole schema and re-apply
all five up migrations:

```sql
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
```

then re-apply:

```bash
cat migrations/*.up.sql | docker compose exec -T postgres psql -U miracle -d egentop_test
```

## Verify Before Pushing

```bash
go build ./...
go vet ./...
go test -count=1 ./...
```

## Useful Commands

| Task | Command |
|------|---------|
| Tail API logs | `docker compose logs -f postgres` (for the DB) |
| Drop into psql | `docker compose exec postgres psql -U miracle -d egentop_db` |
| List tables | `docker compose exec postgres psql -U miracle -d egentop_db -c '\dt'` |
| Reset database volume | `docker compose down -v && docker compose up -d` |
