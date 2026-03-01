.PHONY: build run clean deps test

BINARY := argus
BUILD_DIR := build
MAIN := cmd/argus/main.go

# Build a static binary
build: deps
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./$(MAIN)

# Download Go module dependencies
deps:
	go mod tidy
	go mod download

# Download frontend JS/CSS dependencies
frontend-deps:
	bash download-deps.sh

# Run in development mode
run:
	DATA_PATH=./dev-data/images \
	DB_PATH=./dev-data/db/argus.db \
	LISTEN_ADDR=:8080 \
	go run ./$(MAIN)

# Run with specific config
run-prod:
	./$(BUILD_DIR)/$(BINARY)

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -rf dev-data

# Run tests
test:
	go test ./...

# Build for all target platforms
release: deps
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./$(MAIN)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./$(MAIN)

# Full setup from scratch
setup: deps frontend-deps build
	@echo "Build complete. Binary at $(BUILD_DIR)/$(BINARY)"
	@echo "Run with: make run"
