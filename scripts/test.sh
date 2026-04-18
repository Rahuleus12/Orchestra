#!/usr/bin/env bash
#
# Orchestra — Multi-Agent AI Orchestration Engine
# Test Script
#
# Usage:
#   ./scripts/test.sh                # Run all tests
#   ./scripts/test.sh -v             # Verbose output
#   ./scripts/test.sh -c             # Generate coverage report
#   ./scripts/test.sh -p message     # Run tests for specific package
#   ./scripts/test.sh -r             # Run tests with race detector
#   ./scripts/test.sh -s             # Run short tests only
#   ./scripts/test.sh --integration  # Run integration tests
#   ./scripts/test.sh --watch        # Watch for changes and re-run

set -euo pipefail

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default settings
VERBOSE=false
COVERAGE=false
RACE=true
SHORT=false
WATCH=false
INTEGRATION=false
SPECIFIC_PKG=""
TARGET="./..."
TIMEOUT="10m"
COVERAGE_DIR="${PROJECT_ROOT}/coverage"
COVERAGE_FILE="${COVERAGE_DIR}/coverage.out"
COVERAGE_HTML="${COVERAGE_DIR}/coverage.html"
COVERAGE_THRESHOLD=80

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# ---------------------------------------------------------------------------
# Functions
# ---------------------------------------------------------------------------

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

fail() {
    echo -e "${RED}[FAIL]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

usage() {
    cat <<EOF
Orchestra Test Script

Usage:
  $(basename "$0") [options]

Options:
  -v, --verbose         Verbose test output
  -c, --coverage        Generate coverage report (HTML + terminal)
  -r, --race            Enable race detector (default: enabled)
  -R, --no-race         Disable race detector
  -s, --short           Run only short tests
  -p, --pkg string      Run tests for a specific package (e.g., "message", "provider")
  -t, --timeout string  Test timeout duration (default: "10m")
  -w, --watch           Watch for file changes and re-run tests
      --integration     Run integration tests (requires external services)
      --threshold int   Coverage threshold percentage (default: 80)
  -h, --help            Show this help message

Packages:
  message     internal/message
  provider    internal/provider
  config      internal/config
  mock        internal/provider/mock
  all         ./... (default)

Examples:
  $(basename "$0")                       # Run all tests
  $(basename "$0") -v -c                 # Verbose with coverage
  $(basename "$0") -p message            # Test message package only
  $(basename "$0") -p provider -v        # Test provider package with verbose output
  $(basename "$0") -c --threshold 90     # Coverage with 90% threshold
  $(basename "$0") -R -s                 # No race detector, short tests only
EOF
}

resolve_package() {
    local pkg="$1"
    case "${pkg}" in
        message)
            echo "./internal/message/..."
            ;;
        provider)
            echo "./internal/provider/..."
            ;;
        mock)
            echo "./internal/provider/mock/..."
            ;;
        config)
            echo "./internal/config/..."
            ;;
        all|"")
            echo "./..."
            ;;
        *)
            # Allow raw package paths
            echo "${pkg}"
            ;;
    esac
}

run_tests() {
    local target="$1"
    local start_time
    start_time=$(date +%s)

    echo ""
    info "Running tests..."
    info "  Target:   ${target}"
    info "  Timeout:  ${TIMEOUT}"
    info "  Race:     ${RACE}"
    if [ "${SHORT}" = true ]; then
        info "  Short:    yes"
    fi
    if [ "${INTEGRATION}" = true ]; then
        info "  Tags:     integration"
    fi
    echo ""

    # Build the go test command
    local cmd="go test"

    cmd="${cmd} -count=1"
    cmd="${cmd} -timeout=${TIMEOUT}"

    if [ "${RACE}" = true ]; then
        cmd="${cmd} -race"
    fi

    if [ "${VERBOSE}" = true ]; then
        cmd="${cmd} -v"
    fi

    if [ "${SHORT}" = true ]; then
        cmd="${cmd} -short"
    fi

    if [ "${COVERAGE}" = true ]; then
        mkdir -p "${COVERAGE_DIR}"
        cmd="${cmd} -coverprofile=${COVERAGE_FILE}"
        cmd="${cmd} -covermode=atomic"
    fi

    if [ "${INTEGRATION}" = true ]; then
        cmd="${cmd} -tags=integration"
    fi

    cmd="${cmd} ${target}"

    # Run the tests
    cd "${PROJECT_ROOT}"

    local exit_code=0
    eval "${cmd}" || exit_code=$?

    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo ""

    if [ ${exit_code} -eq 0 ]; then
        success "All tests passed in ${duration}s"
    else
        fail "Tests failed (exit code: ${exit_code}) after ${duration}s"
        return ${exit_code}
    fi

    # Coverage report
    if [ "${COVERAGE}" = true ]; then
        echo ""
        generate_coverage_report
    fi

    return 0
}

generate_coverage_report() {
    if [ ! -f "${COVERAGE_FILE}" ]; then
        warn "Coverage file not found: ${COVERAGE_FILE}"
        return
    fi

    info "Generating coverage report..."

    # Terminal summary
    echo ""
    echo -e "${CYAN}=== Coverage by Function ===${NC}"
    go tool cover -func="${COVERAGE_FILE}"

    # Total coverage
    local total_coverage
    total_coverage=$(go tool cover -func="${COVERAGE_FILE}" | tail -1 | awk '{print $3}' | sed 's/%//')
    echo ""
    info "Total coverage: ${total_coverage}%"

    # Check threshold
    local threshold_int
    threshold_int=$(echo "${COVERAGE_THRESHOLD}" | cut -d. -f1)
    local coverage_int
    coverage_int=$(echo "${total_coverage}" | cut -d. -f1)

    if [ "${coverage_int}" -lt "${threshold_int}" ]; then
        warn "Coverage ${total_coverage}% is below threshold ${COVERAGE_THRESHOLD}%"
    else
        success "Coverage meets threshold of ${COVERAGE_THRESHOLD}%"
    fi

    # HTML report
    go tool cover -html="${COVERAGE_FILE}" -o "${COVERAGE_HTML}"
    success "HTML coverage report: ${COVERAGE_HTML}"
}

watch_tests() {
    info "Watching for file changes..."

    # Check for fswatch or similar
    if ! command -v fswatch &>/dev/null; then
        if ! command -v inotifywait &>/dev/null; then
            error "Neither fswatch nor inotifywait found. Install one to use --watch:\n  macOS: brew install fswatch\n  Linux: apt-get install inotify-tools"
        fi
    fi

    local target
    target=$(resolve_package "${SPECIFIC_PKG}")

    info "Watching: ${PROJECT_ROOT}/internal/"
    info "Press Ctrl+C to stop"
    echo ""

    if command -v fswatch &>/dev/null; then
        fswatch -o "${PROJECT_ROOT}/internal/" "${PROJECT_ROOT}/pkg/" | while read -r _; do
            clear
            echo -e "${CYAN}=== Change detected, running tests ===${NC}"
            echo ""
            run_tests "${target}" || true
            echo ""
            info "Waiting for changes..."
        done
    elif command -v inotifywait &>/dev/null; then
        inotifywait -m -r -e modify,create,delete "${PROJECT_ROOT}/internal/" "${PROJECT_ROOT}/pkg/" | while read -r _; do
            clear
            echo -e "${CYAN}=== Change detected, running tests ===${NC}"
            echo ""
            run_tests "${target}" || true
            echo ""
            info "Waiting for changes..."
        done
    fi
}

# ---------------------------------------------------------------------------
# Parse Arguments
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -c|--coverage)
            COVERAGE=true
            shift
            ;;
        -r|--race)
            RACE=true
            shift
            ;;
        -R|--no-race)
            RACE=false
            shift
            ;;
        -s|--short)
            SHORT=true
            shift
            ;;
        -p|--pkg)
            SPECIFIC_PKG="$2"
            shift 2
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -w|--watch)
            WATCH=true
            shift
            ;;
        --integration)
            INTEGRATION=true
            shift
            ;;
        --threshold)
            COVERAGE_THRESHOLD="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            error "Unknown option: $1\nRun '$(basename "$0") --help' for usage"
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

echo -e "${CYAN}Orchestra — Test Suite${NC}"
echo -e "${CYAN}======================${NC}"

cd "${PROJECT_ROOT}"

# Check Go installation
if ! command -v go &>/dev/null; then
    error "Go is not installed. Please install Go 1.24+ and try again."
fi

go_version=$(go version | awk '{print $3}')
info "Using ${go_version}"

# Resolve target package
TARGET=$(resolve_package "${SPECIFIC_PKG}")

# Watch mode
if [ "${WATCH}" = true ]; then
    watch_tests
    exit 0
fi

# Run tests
run_tests "${TARGET}"
exit_code=$?

echo ""
if [ ${exit_code} -eq 0 ]; then
    success "All tests passed!"
else
    fail "Some tests failed."
    exit ${exit_code}
fi
