#!/usr/bin/env sh
# authz_decisions cleanup — retention for the authorization audit table.
#
# Deletes rows older than 90 days (bounds table growth; nothing references
# authz_decisions, so pruning is safe). Reads DB_URL from the production env
# file so no extra secret handling is needed.
#
# Install:
#   sudo install -m 0755 -o root -g root deploy/cron/authz-decisions-cleanup.sh /usr/local/bin/authz-decisions-cleanup.sh
#   sudo install -m 0644 -o root -g root deploy/cron/egentop-authz-cleanup.cron.example /etc/cron.d/egentop-authz-cleanup
#
# Manual dry run (prints the count that would be deleted):
#   sudo -u root sh -c 'DB_URL="$(sed -n "s/^DB_URL=//p" /etc/egentop/egentop.env)" \
#     psql "$DB_URL" -c "SELECT count(*) FROM authz_decisions WHERE created_at < NOW() - INTERVAL '\''90 days'\'';'
#
# Note: the env file values must be unquoted (as in egentop.env.example).
set -eu

ENV_FILE=${EGENTOP_ENV_FILE:-/etc/egentop/egentop.env}

if [ ! -f "$ENV_FILE" ]; then
    echo "egentop env file not found: $ENV_FILE" >&2
    exit 1
fi

DB_URL=$(sed -n 's/^DB_URL=//p' "$ENV_FILE" | head -n1)

if [ -z "$DB_URL" ]; then
    echo "DB_URL not set in $ENV_FILE" >&2
    exit 1
fi

psql "$DB_URL" -v ON_ERROR_STOP=1 -c \
    "DELETE FROM authz_decisions WHERE created_at < NOW() - INTERVAL '90 days';"
