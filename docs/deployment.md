# Deployment Guide

## Build

The API is a single static Go binary.

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o egentop ./cmd/api
```

For production you should cross-compile for the target platform, e.g.:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o egentop ./cmd/api
```

## Production Configuration

Set these in the environment (never commit secrets):

| Variable | Production value |
|----------|------------------|
| `APP_ENV` | `production` |
| `DB_URL` | Full PostgreSQL URL with TLS if supported |
| `JWT_SECRET` | Long random secret, >= 32 chars |
| `JWT_ACCESS_TTL` | e.g. `15m` |
| `JWT_REFRESH_TTL` | e.g. `720h` |
| `REQUIRE_EMAIL_VERIFICATION` | `true` if email verification is required |
| `CORS_ALLOWED_ORIGINS` | Your dashboard origin(s), comma-separated |
| `LOG_LEVEL` | `info` |

In `production` mode the app sets the `Secure` flag on the refresh-token cookie,
so the API must be served over HTTPS.

## Database

### Migrations

Apply migrations before deploying a new binary, using the same ordered list:

```bash
cat migrations/*.up.sql | psql "$DB_URL"
```

For rollback:

```bash
cat migrations/000003_add_permissions.down.sql \
    migrations/000002_create_projects.down.sql \
    migrations/000001_init_schema.down.sql | psql "$DB_URL"
```

Back up the database before any migration in a production environment.

### Connection Pool

The app opens a `database/sql` connection pool against `DB_URL`. Tune the pool
via the standard `SetMaxOpenConns`/`SetMaxIdleConns` knobs in `pkg/db` if the
defaults are not suitable for your traffic.

## Running

### Option A: Systemd

```ini
[Unit]
Description=Egentop Core API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/egentop/egentop.env
ExecStart=/usr/local/bin/egentop
Restart=always
RestartSec=5
User=egentop
Group=egentop
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

### Option B: Docker

Build the image from the included `Dockerfile` (add one if not present) and run:

```bash
docker run -d \
  --name egentop \
  --env-file /etc/egentop/egentop.env \
  --restart unless-stopped \
  -p 8080:8080 \
  egentop:latest
```

The container only runs the API. Run PostgreSQL separately (managed Postgres
provider or a database container) and point `DB_URL` at it.

## Reverse Proxy

Terminate TLS at a reverse proxy (nginx, Caddy, or a cloud load balancer) and
proxy to `localhost:8080`.

Sample nginx fragment:

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Health Checks

Use the readiness probe for orchestration:

- `GET /v1/ready` — `200` when the database is reachable, `503` otherwise.
- `GET /v1/live` — always `200`; useful for liveness checks.
- `GET /v1/health` — always `200`; informational.

Example orchestration probe:

```yaml
readinessProbe:
  httpGet:
    path: /v1/ready
    port: 8080
```

## Operational Notes

- **Rate limiting is in-memory** (per instance). Under horizontal scaling,
  enforce limits at the load balancer or WAF for consistency.
- **Sessions are server-side** in PostgreSQL; a fresh instance can validate
  existing refresh tokens immediately.
- The app shuts down gracefully on `SIGINT`/`SIGTERM` (10s timeout) and closes
  the database pool.
