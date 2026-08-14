# syntax=docker/dockerfile:1

# Egentop API — multi-stage build.
#
# Build stage: Go 1.26 (matches the `go 1.26.2` directive in go.mod), CGO off
# for a fully static binary. Runtime stage: minimal alpine with a non-root
# user. Distroless would save nothing meaningful here and has no wget for
# HEALTHCHECK, so alpine + busybox wget is used instead.
#
# Note: go.mod contains `replace github.com/mcchukwu/egentop => /home/...`
# which does not exist in this image. It is a no-op for the main module (the
# main module always resolves to its own directory), so builds are unaffected.

# ---- build stage ------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache module downloads as a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Build the static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/egentop ./cmd/api

# ---- runtime stage ----------------------------------------------------------
FROM alpine:3.22

# ca-certificates for TLS to remote databases; postgresql-client (psql) so the
# authz_decisions retention cleanup can run inside the container via
# `docker compose exec` (see deploy/docker/egentop-compose.prod.yaml).
RUN apk add --no-cache ca-certificates postgresql-client \
    && addgroup -S egentop \
    && adduser -S -G egentop egentop

COPY --from=builder /out/egentop /usr/local/bin/egentop

USER egentop

EXPOSE 8080

# The app binds :8080 inside the container; the host must never publish this
# port publicly — the nginx proxy is the only public entry point (bind to
# 127.0.0.1 on the host, or use an internal docker network).
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/v1/live || exit 1

ENTRYPOINT ["/usr/local/bin/egentop"]
