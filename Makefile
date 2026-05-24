.PHONY: build run test clean docker install deps

# Build variables
BINARY_NAME=gopulse
BUILD_DIR=bin
MAIN_PATH=cmd/server/main.go

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for production (optimized, no debug info)
build-prod:
	@echo "Building $(BINARY_NAME) for production..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Production build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run the application
run:
	@go run $(MAIN_PATH)

# Run with file watching (requires air)
dev:
	@air

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

# Build Docker image
docker:
	@echo "Building Docker image..."
	@docker build -t gopulse:latest .

# Run Docker container
docker-run:
	@docker run -d \
		--name gopulse \
		-p 8080:8080 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		gopulse:latest

# Install the binary to /usr/local/bin
install: build-prod
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installation complete"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	@golangci-lint run

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  build-prod     - Build optimized production binary"
	@echo "  run            - Run the application"
	@echo "  dev            - Run with hot reload (requires air)"
	@echo "  deps           - Install dependencies"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  clean          - Clean build artifacts"
	@echo "  docker         - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  install        - Install binary to /usr/local/bin"
	@echo "  fmt            - Format code"
	@echo "  lint           - Lint code"
