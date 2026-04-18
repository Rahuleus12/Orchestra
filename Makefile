.PHONY: all build test lint fmt cover clean vet run tidy check

# Variables
BINARY_NAME=orchestra
BUILD_DIR=./bin
GO=go
GOFLAGS=-v
LINTER=golangci-lint
MAIN_PKG=./cmd/orchestra

# Go source directories
SRC_DIRS=cmd internal pkg

# Default target
all: lint vet test build

## build: Build the orchestra binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PKG)

## run: Run the orchestra binary
run: build
	$(BUILD_DIR)/$(BINARY_NAME)

## test: Run all tests
test:
	@echo "Running tests..."
	$(GO) test ./... -count=1 -race

## test-verbose: Run all tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	$(GO) test ./... -count=1 -race -v

## cover: Run tests with coverage report
cover:
	@echo "Running tests with coverage..."
	$(GO) test ./... -count=1 -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@$(GO) tool cover -func=coverage.out | tail -1

## cover-terminal: Show coverage summary in terminal
cover-terminal:
	@echo "Running tests with coverage (terminal)..."
	$(GO) test ./... -count=1 -race -coverprofile=coverage.out -covermode=atomic
	@$(GO) tool cover -func=coverage.out

## lint: Run golangci-lint
lint:
	@echo "Running linter..."
	@which $(LINTER) > /dev/null 2>&1 || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	$(LINTER) run ./...

## lint-fix: Run golangci-lint with auto-fix
lint-fix:
	@echo "Running linter with auto-fix..."
	$(LINTER) run --fix ./...

## fmt: Format Go source files
fmt:
	@echo "Formatting Go files..."
	@gofmt -l -w $(SRC_DIRS)
	@echo "Formatting complete."

## fmt-check: Check if Go files are formatted (CI-friendly)
fmt-check:
	@echo "Checking formatting..."
	@unformatted=$$(gofmt -l $(SRC_DIRS)); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "All files are properly formatted."

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## tidy: Run go mod tidy
tidy:
	@echo "Tidying go modules..."
	$(GO) mod tidy

## check: Run all checks (fmt-check, lint, vet, test)
check: fmt-check lint vet test

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	@echo "Clean complete."

## help: Show this help message
help:
	@echo "Orchestra — Multi-Agent AI Orchestration Engine"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
