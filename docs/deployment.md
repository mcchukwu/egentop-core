# Deployment Guide

> **Scope:** a minimal **validation instance** so 1–2 friendly agencies in
> Nigeria/West Africa can use the API (client approval deep link + one-time
> credentials shared over WhatsApp). Target cost class: ~$5–10/mo for a single
> VPS with Docker (or a minimal PaaS). This guide is provider-portable; the
> design below works on any VPS provider.

```
                     public internet
                           │ 443 (HTTPS)
                           ▼
        ┌─────────────────────────────────┐
        │  nginx (host service, TLS)      │  ← ONLY public entry point
        │  - certbot TLS                  │
        │  - sanitizes X-Forwarded-For    │  ← HIGH-1 fix (see below)
        │  - edge rate limit /v1/auth/*   │
        └──────────────┬──────────────────┘
                       │ 127.0.0.1:8080 (loopback only)
        ┌──────────────▼──────────────────┐
        │  egentop API (Docker container  │
        │  or systemd bare binary)        │
        └──────────────┬──────────────────┘
                       │ internal network / loopback
        ┌──────────────▼──────────────────┐
        │  PostgreSQL 18                  │  ← never published publicly
        └─────────────────────────────────┘
```

## Build

The API is a single static Go binary.

### Bare binary (systemd path)

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o egentop ./cmd/api
```

### Docker image

```bash
docker build -t egentop:validation-1 .
```

See the `Dockerfile`: multi-stage (`golang:1.26-alpine` build → `alpine:3.22`
runtime), `CGO_ENABLED=0`, non-root user, `EXPOSE 8080`, HEALTHCHECK against
`/v1/live`. The runtime image includes `psql` (for the in-container retention
cleanup) and CA certificates. The runtime image contains only the static
binary — no sources, tests, migrations, or docs.

## Production Configuration

Set these in the environment (never commit secrets). **This is the complete
list of variables the application reads** (verified against
`pkg/config/config.go`):

| Variable | Production value |
|----------|------------------|
| `APP_ENV` | `production` |
| `APP_PORT` | `8080` (never exposed publicly) |
| `DB_URL` | PostgreSQL DSN; `sslmode=require` for remote, `sslmode=disable` only for loopback/internal-network Postgres |
| `JWT_SECRET` | Long random secret, **>= 32 chars** (see below) |
| `JWT_ACCESS_TTL` | e.g. `15m` |
| `JWT_REFRESH_TTL` | e.g. `720h` |
| `CORS_ALLOWED_ORIGINS` | The **real** client origin(s), comma-separated, no wildcards |

Template: [`deploy/env/egentop.env.example`](../deploy/env/egentop.env.example)
— install to `/etc/egentop/egentop.env` (root:egentop, mode 0640).

> **Documented but NOT read by the running app** (config surface verified
> 2026-08-14): `LOG_LEVEL`, `APP_NAME`, `DB_HOST`/`DB_PORT`/`DB_NAME`/
> `DB_USER`/`DB_PASSWORD`/`DB_SSLMODE` (Makefile-only), and
> `RATE_LIMIT_REQUESTS`/`RATE_LIMIT_WINDOW` (dead config, security-review
> LOW-2). Do not expect these to change behavior. If the team wants
> `LOG_LEVEL` or configurable rate limits, that is a Builder-batch code item.

### Secrets

```bash
# JWT signing secret (>= 32 chars, random)
openssl rand -base64 48
```

In `production` mode the app sets the `Secure` flag on the refresh-token
cookie, so the API must be served over HTTPS (nginx handles this).

## Database

### Deployment paths

- **Compose path (primary):** Postgres runs as a container in the same
  compose project, **with no published ports** — reachable only from the app
  container. `DB_URL=postgres://…@postgres:5432/…` (`sslmode=disable`; the
  traffic never leaves the host). See
  [`deploy/docker/egentop-compose.prod.yaml`](../deploy/docker/egentop-compose.prod.yaml).
- **Systemd path:** Postgres installed on the VPS (`apt install postgresql`),
  bound to `127.0.0.1` only, `DB_URL=postgres://…@127.0.0.1:5432/…`.
- **Managed/remote Postgres:** acceptable, but use `sslmode=require` and an
  IP allowlist restricted to the VPS. The database must **never** be publicly
  reachable (no `0.0.0.0` binds, no open firewall ports).

### Migrations

Apply migrations **before** starting a new binary, using the same ordered
list (`-v ON_ERROR_STOP=1` makes a failed statement fail loudly instead of
leaving a partial schema):

```bash
cat migrations/*.up.sql | psql -v ON_ERROR_STOP=1 "$DB_URL"
```

For rollback, apply the down migrations in reverse order. The example below
rolls back all five migrations:

```bash
cat migrations/000005_layer1_delta.down.sql \
    migrations/000004_add_session_token_lookup_hash.down.sql \
    migrations/000003_add_permissions.down.sql \
    migrations/000002_create_projects.down.sql \
    migrations/000001_init_schema.down.sql | psql "$DB_URL"
```

Back up the database before any migration in a production environment.

### Connection Pool

The app opens a `database/sql` connection pool against `DB_URL`. Tune the pool
via the standard `SetMaxOpenConns`/`SetMaxIdleConns` knobs in `pkg/db` if the
defaults are not suitable for your traffic.

### Database Maintenance

**Retention: `authz_decisions`**

Authorization decisions are append-only audit rows that grow by one per
request. Prune them on a schedule (e.g. weekly) with:

```sql
DELETE FROM authz_decisions
WHERE created_at < NOW() - INTERVAL '90 days';
```

90 days is the recommended default: it bounds table size and keeps
`idx_authz_decisions_org_created` scans fast while still covering a
~quarter-year investigation window. `permission_key` and `reason` are
denormalized snapshots and nothing references `authz_decisions`, so pruning
is safe. Extend the interval (180/365 days) if compliance or post-incident
forensics need a longer window; shorten it if storage is a concern.

**Automation (cron):**

- Script: [`deploy/cron/authz-decisions-cleanup.sh`](../deploy/cron/authz-decisions-cleanup.sh)
  (reads `DB_URL` from `/etc/egentop/egentop.env`).
- Crontab entry: [`deploy/cron/egentop-authz-cleanup.cron.example`](../deploy/cron/egentop-authz-cleanup.cron.example)
  — weekly, Sunday 03:30. Install into `/etc/cron.d/`.

```bash
sudo install -m 0755 -o root -g root deploy/cron/authz-decisions-cleanup.sh /usr/local/bin/authz-decisions-cleanup.sh
sudo install -m 0644 -o root -g root deploy/cron/egentop-authz-cleanup.cron.example /etc/cron.d/egentop-authz-cleanup
```

In the compose path the script runs **inside the app container** (it bundles
psql and has `postgres` in its network):

```bash
30 3 * * 0 root docker compose -f /opt/egentop/deploy/docker/egentop-compose.prod.yaml \
    exec -T egentop /usr/local/bin/authz-decisions-cleanup.sh \
    >> /var/log/egentop-authz-cleanup.log 2>&1
```

Locally, `make authz-decisions-cleanup` runs the same DELETE using the `.env`
DSN.

## Reverse Proxy (nginx)

Terminate TLS at nginx and proxy to `localhost:8080`. **Full working config:
[`deploy/nginx/egentop.conf`](../deploy/nginx/egentop.conf)** — TLS server
block, HTTP→HTTPS redirect, HSTS, edge rate limiting, `client_max_body_size 1m`
(the app has no body-size limit of its own; security-review MEDIUM-3).

**Corrected `X-Forwarded-For` handling (security-review HIGH-1).**

The app's in-memory rate limiter keys on the `X-Forwarded-For` header
**verbatim** (`getClientIP` returns the header value if present). The old
sample used:

```nginx
# INSECURE — do not use:
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

`$proxy_add_x_forwarded_for` *appends* the client's IP to any value the
attacker already sent. Because the app keys on the whole header string, each
forged value (`X-Forwarded-For: 1.2.3.4`, `1.2.3.5`, …) creates a fresh
rate-limit bucket — a bypass — and an unbounded in-memory map (memory DoS).

The corrected proxy **overwrites** the header with the IP nginx actually saw
on the TCP connection (`$remote_addr`), which a client cannot forge:

```nginx
proxy_set_header X-Forwarded-For   $remote_addr;
proxy_set_header X-Real-IP         $remote_addr;
```

Edge rate limiting on the auth surface is added in the same file:

```nginx
limit_req_zone $binary_remote_addr zone=egentop_auth:10m rate=20r/m;

location /v1/auth/ {
    limit_req zone=egentop_auth burst=10 nodelay;
    ...
}
```

The edge zone is deliberately looser than the app's per-endpoint limits
(login 5/min, register 3/min, refresh 10/min, password change 5/min); it is
the first line of defense, not a replacement for them.

Install:

```bash
sudo apt install nginx
sudo install -m 0644 deploy/nginx/egentop.conf /etc/nginx/sites-available/egentop.conf
sudo ln -s ../sites-available/egentop.conf /etc/nginx/sites-enabled/egentop.conf
sudo nginx -t && sudo systemctl reload nginx
sudo certbot certonly --nginx -d api.example.com
```

> If you later put Cloudflare in front of the VPS, nginx will see Cloudflare's
> IP as `$remote_addr`; key the rate-limit zone on `$http_cf_connecting_ip`
> instead (see the comment in the config).

## Deployment Paths

### Path A — VPS with Docker + host nginx (primary, recommended)

Provider: any ~$5–10/mo Linux VPS with Docker. From an empty VPS:

```bash
# 1. Baseline
apt update && apt upgrade -y
apt install -y docker.io docker-compose-plugin nginx ufw
ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443/tcp && ufw enable

# 2. Install the app (clone the repo, or copy the deploy/ artifacts + build image)
git clone https://github.com/mcchukwu/egentop /opt/egentop
cd /opt/egentop

# 3. Secrets + env file
sudo install -d /etc/egentop -o root -g root -m 750
sudo install -m 0640 deploy/env/egentop.env.example /etc/egentop/egentop.env
sudoedit /etc/egentop/egentop.env        # APP_ENV=production, JWT_SECRET, DB_URL, CORS...
# export POSTGRES_PASSWORD='<a different strong password>'   # for the compose DB

# 4. Start app + database (Postgres has no public port)
docker compose -f deploy/docker/egentop-compose.prod.yaml up -d --build

# 5. Migrations (apply against the compose DB — exec into the container network)
docker compose -f deploy/docker/egentop-compose.prod.yaml \
  exec -T postgres psql -U egentop -d egentop < migrations/000001_init_schema.up.sql
# ...repeat for 000002..000005, or apply the concatenated file the same way:
cat migrations/*.up.sql | \
  docker compose -f deploy/docker/egentop-compose.prod.yaml exec -T postgres psql -U egentop -d egentop -v ON_ERROR_STOP=1

# 6. nginx + TLS (points at 127.0.0.1:8080)
sudo install -m 0644 deploy/nginx/egentop.conf /etc/nginx/sites-available/egentop.conf
sudo ln -s ../sites-available/egentop.conf /etc/nginx/sites-enabled/egentop.conf
sudo nginx -t && sudo systemctl reload nginx
sudo certbot certonly --nginx -d api.example.com

# 7. Retention cron (weekly cleanup of authz_decisions)
sudo install -m 0644 -o root -g root deploy/cron/egentop-authz-cleanup.cron.example /etc/cron.d/egentop-authz-cleanup
# (the /etc/cron.d entry must reference the compose path — see the compose file header)

# 8. Verify — see the smoke-test checklist below.
```

### Path B — Bare binary + systemd + host nginx

For operators who prefer no app container:

```bash
apt install -y nginx postgresql ufw
# create DB + user, bind postgres to 127.0.0.1
sudo useradd --system --no-create-home --shell /usr/sbin/nologin egentop
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o egentop ./cmd/api
sudo install -m 0755 -o root -g root egentop /usr/local/bin/egentop
sudo install -d /etc/egentop -o root -g egentop -m 750
sudo install -m 0640 deploy/env/egentop.env.example /etc/egentop/egentop.env   # then edit
sudo install -m 0644 deploy/systemd/egentop.service /etc/systemd/system/egentop.service
sudo systemctl daemon-reload && sudo systemctl enable --now egentop
# nginx + certbot + cron: same as Path A steps 6–7
```

The unit (`deploy/systemd/egentop.service`) includes hardening
(`NoNewPrivileges`, `ProtectSystem=strict`, `CapabilityBoundingSet=`, …) and
graceful-shutdown handling (`TimeoutStopSec=20` > the app's 10s drain).

### Path C — Minimal PaaS (noted)

Render/Railway/Fly.io-style PaaS works for the app container, but you lose
control of the nginx layer. If you use one, meet the same checklist by
putting the **same nginx config on a reverse-proxy front** (or the PaaS's TLS
termination + a proxy product) so X-Forwarded-For is still sanitized and the
edge rate limit still applies. The bare-binary/systemd and compose artifacts
in this repo are the tested paths; a PaaS deployment must re-verify the
smoke-test checklist below.

## Provider Guidance (~$5–10/mo, single instance)

- **Hetzner** — CX22/CPX11 class (≈€4–6/mo): reliable, generous bandwidth,
  good Linux support. Regions: Falkenstein/Nuremberg/Helsinki (~60–90 ms RTT
  to Lagos/Accra — fine for a JSON API).
- **DigitalOcean / Vultr / Linode** — 1 vCPU / 1 GB droplet class ($5–6/mo):
  simple, well-documented. London or Frankfurt regions minimize West-Africa
  latency.
- **Oracle Cloud Free Tier (Ampere A1)** — $0/mo, but signup friction and
  idle-reclaim policy; fine as a spare, not the primary plan.
- Prices change; compare at purchase time. The artifacts here are
  provider-portable: any Debian/Ubuntu VPS with Docker works.

## Health Checks

- `GET /v1/ready` — `200` when the database is reachable, `503` otherwise.
- `GET /v1/live` — always `200`; used by the container HEALTHCHECK.
- `GET /v1/health` — always `200`; informational.

```bash
curl -fsS https://api.example.com/v1/live   # 200
curl -fsS https://api.example.com/v1/ready  # 200 when DB is up
```

## Smoke-Test Checklist (deploy gate — from the security review)

Run every item against the live instance before sharing credentials with an
agency. [ ] = must pass.

- [ ] **HTTPS only** — `http://api.example.com` redirects (301) to HTTPS; no
      plaintext service on 8080 (`curl -sI http://api.example.com:8080` times
      out / is refused).
- [ ] **TLS valid** — `curl -fsS https://api.example.com/v1/live` returns
      200 with a trusted cert (certbot).
- [ ] **Health** — `/v1/live` and `/v1/ready` both 200.
- [ ] **XFF sanitization (HIGH-1)** — send a forged header:
      `curl -s -H 'X-Forwarded-For: 203.0.113.9' https://api.example.com/v1/auth/login -d '{}'`
      six times quickly; you must still hit the app's 429 limit (the key is
      your real IP, not the forged value). Repeat with two different forged
      values and confirm the limit is still hit at the same count.
- [ ] **Edge rate limit** — hammering `/v1/auth/*` from one IP returns 429 at
      the nginx zone limit.
- [ ] **Secure cookie** — after `POST /v1/auth/login` with valid credentials,
      the response's `Set-Cookie` for the refresh token includes `Secure`
      (and `HttpOnly`). Requires `APP_ENV=production`.
- [ ] **Auth flow** — register → login → refresh (cookie rotation) → logout;
      a replayed refresh token is rejected (family revocation).
- [ ] **Client deep link** — provision a client, share the one-time
      credential, confirm first-login forces password change and the approval
      deep link loads.
- [ ] **CORS** — `OPTIONS` preflight from the real origin passes; a request
      with an unknown `Origin` gets no CORS headers.
- [ ] **Database not public** — `nc -zv <vps-ip> 5432` from outside fails;
      Postgres has no public bind (`ss -tlnp | grep 5432` shows 127.0.0.1 or
      a compose bridge, never 0.0.0.0 on a public interface).
- [ ] **JWT_SECRET** — >= 32 chars, random (`openssl rand -base64 48`),
      present only in `/etc/egentop/egentop.env`.
- [ ] **authz_decisions cleanup** — cron installed; run the cleanup script
      manually once and confirm the DELETE executes without error.
- [ ] **Graceful shutdown** — `systemctl restart egentop` (or
      `docker compose restart egentop`) completes an in-flight request
      instead of dropping it.

## Updating the Instance

1. Apply migrations **first** (up files in order; back up the DB first).
2. Build/pull the new binary or image.
3. Restart the service (graceful — see above).
4. Run the smoke-test checklist.

## Operational Notes

- **Rate limiting is in-memory** (per instance). This is fine for a
  single-instance validation deploy; before scaling to multiple instances,
  move the limiter to a shared store (e.g. Redis) so limits apply across all
  instances. Until then, the nginx edge rate limit is the consistent
  cross-instance defense.
- **Sessions are server-side** in PostgreSQL; a fresh instance can validate
  existing refresh tokens immediately.
- **Backups** — compose path:
  `docker compose -f deploy/docker/egentop-compose.prod.yaml exec -T postgres pg_dump -U egentop egentop > backup_$(date +%F).sql`.
  Systemd path: `pg_dump "$DB_URL"`. Keep the most recent dump off-box.
- The app shuts down gracefully on `SIGINT`/`SIGTERM` (10s timeout) and closes
  the database pool.
