APP_NAME := egentop
CMD_PATH := ./cmd/api
BIN_PATH := bin

.PHONY: run build clean test migrate-up migrate-down authz-decisions-cleanup

run: 
	go run $(CMD_PATH)

build:
	go build -o $(BIN_PATH)/$(APP_NAME) $(CMD_PATH)

clean:
	rm -rf $(BIN_PATH)

test:
	go test ./...

migrate-up:
	export $$(grep -v '^#' .env | xargs) && \
	migrate -path migrations -database "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" up

migrate-down:
	export $$(grep -v '^#' .env | xargs) && \
	migrate -path migrations -database "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" down 1

authz-decisions-cleanup:
	export $$(grep -v '^#' .env | xargs) && \
	psql "postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" -c "DELETE FROM authz_decisions WHERE created_at < NOW() - INTERVAL '90 days';"
