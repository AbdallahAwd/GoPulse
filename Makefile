.PHONY: build build-prod run dev deps test test-ui test-race check vet fmt-check test-coverage clean docker docker-run install fmt lint security secrets licenses help

# Build variables
BINARY_NAME=gopulse
BUILD_DIR=bin
MAIN_PATH=./cmd/server
TOOLS_DIR=$(CURDIR)/bin/tools

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

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Frontend regression tests (Node.js 22+)
test-ui:
	@node --test web/tests/*.test.cjs

test-race:
	@go test -race ./...

vet:
	@go vet ./...

fmt-check:
	@test -z "$$(gofmt -l cmd internal web)" || (gofmt -l cmd internal web; exit 1)

check: fmt-check vet test test-ui build-prod

security:
	@mkdir -p $(TOOLS_DIR)
	@GOBIN=$(TOOLS_DIR) go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
	@$(TOOLS_DIR)/govulncheck ./...

secrets:
	@mkdir -p $(TOOLS_DIR)
	@GOBIN=$(TOOLS_DIR) go install github.com/zricethezav/gitleaks/v8@v8.30.0
	@GITLEAKS=$(TOOLS_DIR)/gitleaks bash scripts/check-secrets.sh

licenses:
	@mkdir -p $(TOOLS_DIR)
	@GOBIN=$(TOOLS_DIR) go install github.com/google/go-licenses/v2@v2.0.1
	@GOROOT="$$(go env GOROOT)" $(TOOLS_DIR)/go-licenses save ./cmd/server --save_path=third_party_licenses --force --ignore=github.com/AbdallahAwd/GoPulse

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

# Run a remote-management container; .env must contain a persistent JWT secret.
docker-run:
	@docker run -d \
		--name gopulse \
		--env-file .env \
		-e GOPULSE_SERVER_HOST=0.0.0.0 \
		-p 127.0.0.1:8080:8080 \
		-v gopulse-data:/var/lib/gopulse \
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

# Lint using the standard toolchain
lint: fmt-check vet

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
	@echo "  lint           - Check formatting and run go vet"
	@echo "  check          - Format, vet, Go/JS tests and production build"
	@echo "  test-race      - Run Go tests with the race detector"
	@echo "  security       - Scan reachable Go vulnerabilities"
	@echo "  secrets        - Scan working tree and all Git history"
	@echo "  licenses       - Collect dependency license notices"
