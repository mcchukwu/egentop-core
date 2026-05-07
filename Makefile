APP_NAME := egentop
CMD_PATH := ./cmd/api
BIN_PATH := bin

.PHONY: run build clean test

run: 
	go run $(CMD_PATH)

build:
	go build -o $(BIN_PATH)/$(APP_NAME) $(CMD_PATH)

clean:
	rm -rf $(BIN_PATH)

test:
	go test ./...
	
