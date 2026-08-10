APP_NAME := egentop
CMD_PATH := ./cmd/api
BIN_PATH := bin

.PHONY: run build clean test migrate-up migrate-down

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
