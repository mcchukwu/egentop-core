APP_NAME := egentop
CMD_PATH := ./cmd/api
BIN_PATH := bin

.PHONY: run build clean test test-integration migrate-up migrate-down authz-decisions-cleanup

run: 
	go run $(CMD_PATH)

build:
	go build -o $(BIN_PATH)/$(APP_NAME) $(CMD_PATH)

clean:
	rm -rf $(BIN_PATH)

test:
	go test ./...

# test-integration: apply all migrations to the database referenced by
# EGTEST_DB_URL, then run the full test suite (unit + integration) against it.
# The database must already exist (psql does not create it) and should be a
# throwaway database — migrations are not idempotent. Example:
#   EGTEST_DB_URL='postgres://user:pass@localhost:5432/egentop_test' make test-integration
test-integration:
	@test -n "$(EGTEST_DB_URL)" || (echo "EGTEST_DB_URL is required (e.g. postgres://user:pass@localhost:5432/egentop_test)" && exit 1)
	cat migrations/*.up.sql | psql -v ON_ERROR_STOP=1 "$(EGTEST_DB_URL)"
	EGTEST_DB_URL="$(EGTEST_DB_URL)" go test ./...

migrate-up:
	export $$(grep -v '^#' .env | xargs) && \
	migrate -path migrations -database "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" up

migrate-down:
	export $$(grep -v '^#' .env | xargs) && \
	migrate -path migrations -database "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" down 1

authz-decisions-cleanup:
	export $$(grep -v '^#' .env | xargs) && \
	psql "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" -c "DELETE FROM authz_decisions WHERE created_at < NOW() - INTERVAL '90 days';"
