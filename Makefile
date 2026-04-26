.PHONY: all build test lint fmt cover clean vet run tidy check \
        test-ci test-junit test-gotestsum test-parallel \
        security govulncheck gosec \
        test-summary

# Variables
BINARY_NAME=orchestra
BUILD_DIR=./bin
GO=go
GOFLAGS=-v
LINTER=golangci-lint
MAIN_PKG=./cmd/orchestra
GOTESTSUM=gotestsum

# Go source directories
SRC_DIRS=cmd internal pkg

# Test settings
TEST_TIMEOUT?=10m
TEST_RACE?=-race
TEST_COUNT?=-count=1
JUNIT_FILE?=test-results.xml

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
	$(GO) test ./... $(TEST_COUNT) $(TEST_RACE) -timeout=$(TEST_TIMEOUT)

## test-verbose: Run all tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	$(GO) test ./... $(TEST_COUNT) $(TEST_RACE) -timeout=$(TEST_TIMEOUT) -v

## test-ci: Run tests in CI-optimized mode (JUnit XML + gotestsum)
test-ci:
	@echo "Running tests (CI mode)..."
	@which $(GOTESTSUM) > /dev/null 2>&1 || (echo "Installing gotestsum..." && go install gotest.tools/gotestsum@latest)
	$(GOTESTSUM) --format standard-verbose --junitfile=$(JUNIT_FILE) --junitfile-testsuite-name=orchestra -- \
		./... $(TEST_COUNT) $(TEST_RACE) -timeout=$(TEST_TIMEOUT)

## test-junit: Generate JUnit XML output (alias for test-ci)
test-junit: test-ci

## test-gotestsum: Run tests with gotestsum (no JUnit)
test-gotestsum:
	@echo "Running tests (gotestsum)..."
	@which $(GOTESTSUM) > /dev/null 2>&1 || (echo "Installing gotestsum..." && go install gotest.tools/gotestsum@latest)
	$(GOTESTSUM) --format standard-verbose -- ./... $(TEST_COUNT) $(TEST_RACE) -timeout=$(TEST_TIMEOUT)

## test-parallel: Run tests with parallel package execution
test-parallel:
	@echo "Running tests (parallel packages)..."
	$(GO) test -p 4 ./... $(TEST_COUNT) $(TEST_RACE) -timeout=$(TEST_TIMEOUT)

## cover: Run tests with coverage report
cover:
	@echo "Running tests with coverage..."
	$(GO) test ./... $(TEST_COUNT) $(TEST_RACE) -coverprofile=coverage.out -covermode=atomic -timeout=$(TEST_TIMEOUT)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@$(GO) tool cover -func=coverage.out | tail -1

## cover-terminal: Show coverage summary in terminal
cover-terminal:
	@echo "Running tests with coverage (terminal)..."
	$(GO) test ./... $(TEST_COUNT) $(TEST_RACE) -coverprofile=coverage.out -covermode=atomic -timeout=$(TEST_TIMEOUT)
	@$(GO) tool cover -func=coverage.out

## cover-ci: Generate coverage report for CI (with threshold check)
cover-ci: COVER_THRESHOLD?=80
cover-ci:
	@echo "Running tests with coverage (CI)..."
	$(GO) test ./... $(TEST_COUNT) $(TEST_RACE) -coverprofile=coverage.out -covermode=atomic -timeout=$(TEST_TIMEOUT)
	@echo "Coverage by function:"
	@$(GO) tool cover -func=coverage.out
	@COVERAGE=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	echo ""; \
	echo "Total coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < $(COVER_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "::warning::Coverage $${COVERAGE}% is below threshold $(COVER_THRESHOLD)%"; \
	else \
		echo "Coverage meets threshold of $(COVER_THRESHOLD)%"; \
	fi

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

## security: Run all security checks (govulncheck + gosec)
security: govulncheck gosec

## govulncheck: Check for known vulnerabilities in dependencies
govulncheck:
	@echo "Running govulncheck..."
	@which govulncheck > /dev/null 2>&1 || (echo "Installing govulncheck..." && go install golang.org/x/vuln/cmd/govulncheck@latest)
	govulncheck ./...

## gosec: Run security scanner
gosec:
	@echo "Running gosec..."
	@which gosec > /dev/null 2>&1 || (echo "Installing gosec..." && go install github.com/securego/gosec/v2/cmd/gosec@latest)
	gosec -no-fail ./...

## check: Run all checks (fmt-check, lint, vet, test)
check: fmt-check lint vet test

## check-ci: Run all CI checks (fmt-check, lint, vet, test-ci, security)
check-ci: fmt-check lint vet test-ci security

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html $(JUNIT_FILE) gosec-report.json
	@echo "Clean complete."

## test-summary: Print test summary from JUnit results (for CI)
test-summary:
	@if [ -f $(JUNIT_FILE) ]; then \
		echo "## Test Summary"; \
		echo ""; \
		TESTS=$$(grep -oP 'tests="\K[^"]+' $(JUNIT_FILE) 2>/dev/null || echo "0"); \
		FAILURES=$$(grep -oP 'failures="\K[^"]+' $(JUNIT_FILE) 2>/dev/null || echo "0"); \
		ERRORS=$$(grep -oP 'errors="\K[^"]+' $(JUNIT_FILE) 2>/dev/null || echo "0"); \
		echo "Total: $${TESTS}, Failures: $${FAILURES}, Errors: $${ERRORS}"; \
	else \
		echo "No JUnit results found ($(JUNIT_FILE))"; \
	fi

## help: Show this help message
help:
	@echo "Orchestra — Multi-Agent AI Orchestration Engine"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
