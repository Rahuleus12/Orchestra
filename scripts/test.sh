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
#   ./scripts/test.sh --ci           # CI-optimized output (JUnit XML)
#   ./scripts/test.sh --junit        # Generate JUnit XML output

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

# CI-specific settings
CI_MODE=false
JUNIT_OUTPUT=""
USE_GOTESTSUM=false
GOTESTSUM_FORMAT=""
PARALLEL_PACKAGES=1

# Detect CI environment
if [[ -n "${CI:-}" ]] || [[ -n "${GITHUB_ACTIONS:-}" ]] || [[ -n "${JENKINS_URL:-}" ]] || [[ -n "${GITLAB_CI:-}" ]]; then
  CI_MODE=true
fi

# ---------------------------------------------------------------------------
# Colors (disabled in CI unless explicitly requested)
# ---------------------------------------------------------------------------

if [[ "${CI_MODE}" == "true" && -z "${FORCE_COLORS:-}" ]]; then
  RED=''
  GREEN=''
  YELLOW=''
  BLUE=''
  CYAN=''
  NC=''
else
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  BLUE='\033[0;34m'
  CYAN='\033[0;36m'
  NC='\033[0m'
fi

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

# GitHub Actions annotation helpers
gha_notice() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::notice::$1"
    fi
}

gha_warning() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::warning::$1"
    fi
}

gha_error() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::error::$1"
    fi
}

gha_group() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::group::$1"
    else
        echo -e "${CYAN}=== $1 ===${NC}"
    fi
}

gha_endgroup() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::endgroup::"
    fi
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
      --ci              Enable CI-optimized mode (auto-detected in CI)
      --junit FILE      Generate JUnit XML output to FILE
      --gotestsum       Use gotestsum for formatted output
      --parallel N      Run test packages in parallel (default: 1, CI: auto)
      --no-color        Disable colored output
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
  $(basename "$0") --junit results.xml   # Generate JUnit XML for CI
  $(basename "$0") --ci                  # CI-optimized mode
  $(basename "$0") --gotestsum           # Use gotestsum for pretty output

Environment Variables:
  CI                  Set to enable CI mode automatically
  GITHUB_ACTIONS      Set by GitHub Actions, enables CI mode
  FORCE_COLORS        Force colored output even in CI
  GOTESTSUM_FORMAT    Override gotestsum format (default: auto-detect)
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

check_gotestsum() {
    if ! command -v gotestsum &>/dev/null; then
        if [[ "${CI_MODE}" == "true" ]]; then
            warn "gotestsum not found, installing..."
            go install gotest.tools/gotestsum@latest
        else
            warn "gotestsum not found. Install with: go install gotest.tools/gotestsum@latest"
            return 1
        fi
    fi
    return 0
}

detect_gotestsum_format() {
    if [[ -n "${GOTESTSUM_FORMAT:-}" ]]; then
        echo "${GOTESTSUM_FORMAT}"
        return
    fi

    if [[ "${CI_MODE}" == "true" ]]; then
        echo "standard-verbose"
    elif [[ "${VERBOSE}" == "true" ]]; then
        echo "standard-verbose"
    else
        echo "standard-quiet"
    fi
}

run_tests() {
    local target="$1"
    local start_time
    start_time=$(date +%s)

    echo ""
    gha_group "Test Configuration"
    info "  Target:   ${target}"
    info "  Timeout:  ${TIMEOUT}"
    info "  Race:     ${RACE}"
    info "  CI Mode:  ${CI_MODE}"
    if [[ "${SHORT}" == "true" ]]; then
        info "  Short:    yes"
    fi
    if [[ "${INTEGRATION}" == "true" ]]; then
        info "  Tags:     integration"
    fi
    if [[ -n "${JUNIT_OUTPUT}" ]]; then
        info "  JUnit:    ${JUNIT_OUTPUT}"
    fi
    if [[ "${USE_GOTESTSUM}" == "true" ]]; then
        info "  gotestsum: yes"
    fi
    gha_endgroup
    echo ""

    # Build test arguments
    local test_args=()
    test_args+=("-count=1")
    test_args+=("-timeout=${TIMEOUT}")

    if [[ "${RACE}" == "true" ]]; then
        test_args+=("-race")
    fi

    if [[ "${VERBOSE}" == "true" && "${USE_GOTESTSUM}" != "true" ]]; then
        test_args+=("-v")
    fi

    if [[ "${SHORT}" == "true" ]]; then
        test_args+=("-short")
    fi

    if [[ "${COVERAGE}" == "true" ]]; then
        mkdir -p "${COVERAGE_DIR}"
        test_args+=("-coverprofile=${COVERAGE_FILE}")
        test_args+=("-covermode=atomic")
    fi

    if [[ "${INTEGRATION}" == "true" ]]; then
        test_args+=("-tags=integration")
    fi

    # Run the tests
    cd "${PROJECT_ROOT}"

    local exit_code=0

    if [[ "${USE_GOTESTSUM}" == "true" ]] && check_gotestsum; then
        local gotestsum_args=()
        gotestsum_args+=("--format=$(detect_gotestsum_format)")

        if [[ -n "${JUNIT_OUTPUT}" ]]; then
            gotestsum_args+=("--junitfile=${JUNIT_OUTPUT}")
            # Add test suite name for better organization
            local suite_name="orchestra"
            if [[ -n "${SPECIFIC_PKG}" ]]; then
                suite_name="orchestra-${SPECIFIC_PKG}"
            fi
            gotestsum_args+=("--junitfile-testsuite-name=${suite_name}")
        fi

        gha_group "Running Tests (gotestsum)"
        gotestsum "${gotestsum_args[@]}" -- "${test_args[@]}" "${target}" || exit_code=$?
        gha_endgroup
    elif [[ -n "${JUNIT_OUTPUT}" ]] && check_gotestsum; then
        # JUnit output requires gotestsum
        gha_group "Running Tests (JUnit output)"
        gotestsum \
            --format="$(detect_gotestsum_format)" \
            --junitfile="${JUNIT_OUTPUT}" \
            --junitfile-testsuite-name="orchestra" \
            -- \
            "${test_args[@]}" \
            "${target}" || exit_code=$?
        gha_endgroup
    else
        # Standard go test
        gha_group "Running Tests"
        go test -p "${PARALLEL_PACKAGES}" "${test_args[@]}" "${target}" || exit_code=$?
        gha_endgroup
    fi

    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo ""

    if [ ${exit_code} -eq 0 ]; then
        success "All tests passed in ${duration}s"
    else
        fail "Tests failed (exit code: ${exit_code}) after ${duration}s"

        # GitHub Actions: annotate failures
        if [[ -n "${GITHUB_ACTIONS:-}" && -n "${JUNIT_OUTPUT:-}" && -f "${JUNIT_OUTPUT}" ]]; then
            annotate_junit_failures "${JUNIT_OUTPUT}"
        fi

        return ${exit_code}
    fi

    # Coverage report
    if [[ "${COVERAGE}" == "true" ]]; then
        echo ""
        generate_coverage_report
    fi

    return 0
}

annotate_junit_failures() {
    local junit_file="$1"

    if [[ ! -f "${junit_file}" ]]; then
        return
    fi

    # Extract failure information from JUnit XML
    # This is a simple parser - for complex cases, consider using a proper XML tool
    local failures=0
    failures=$(grep -oP 'failures="\K[^"]+' "${junit_file}" 2>/dev/null || echo "0")

    if [[ "${failures}" -gt 0 ]]; then
        gha_warning "Tests failed: ${failures} failure(s) detected"

        # Try to extract and annotate individual test failures
        # This uses basic grep/sed - consider using xmllint for more robust parsing
        if command -v xmllint &>/dev/null; then
            # Use xmllint for proper XML parsing
            local failed_tests
            failed_tests=$(xmllint --xpath "//failure/.." "${junit_file}" 2>/dev/null | \
                grep -oP 'name="\K[^"]+' | head -20 || true)

            for test_name in ${failed_tests}; do
                gha_error "Test failed: ${test_name}"
            done
        fi
    fi
}

generate_coverage_report() {
    if [[ ! -f "${COVERAGE_FILE}" ]]; then
        warn "Coverage file not found: ${COVERAGE_FILE}"
        return
    fi

    gha_group "Coverage Report"
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
        gha_warning "Coverage ${total_coverage}% is below threshold ${COVERAGE_THRESHOLD}%"
    else
        success "Coverage meets threshold of ${COVERAGE_THRESHOLD}%"
    fi

    # HTML report
    go tool cover -html="${COVERAGE_FILE}" -o "${COVERAGE_HTML}"
    success "HTML coverage report: ${COVERAGE_HTML}"

    gha_endgroup

    # GitHub Actions: Set output for workflow consumption
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "coverage=${total_coverage}" >> "$GITHUB_ENV"
    fi
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

print_ci_summary() {
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "## Test Results" >> "$GITHUB_STEP_SUMMARY"
        echo "" >> "$GITHUB_STEP_SUMMARY"

        if [[ -n "${JUNIT_OUTPUT:-}" && -f "${JUNIT_OUTPUT:-}" ]]; then
            local tests failures errors skipped
            tests=$(grep -oP 'tests="\K[^"]+' "${JUNIT_OUTPUT}" 2>/dev/null || echo "0")
            failures=$(grep -oP 'failures="\K[^"]+' "${JUNIT_OUTPUT}" 2>/dev/null || echo "0")
            errors=$(grep -oP 'errors="\K[^"]+' "${JUNIT_OUTPUT}" 2>/dev/null || echo "0")
            skipped=$(grep -oP 'skipped="\K[^"]+' "${JUNIT_OUTPUT}" 2>/dev/null || echo "0")

            echo "| Metric | Count |" >> "$GITHUB_STEP_SUMMARY"
            echo "|--------|-------|" >> "$GITHUB_STEP_SUMMARY"
            echo "| Total Tests | ${tests} |" >> "$GITHUB_STEP_SUMMARY"
            echo "| Failures | ${failures} |" >> "$GITHUB_STEP_SUMMARY"
            echo "| Errors | ${errors} |" >> "$GITHUB_STEP_SUMMARY"
            echo "| Skipped | ${skipped} |" >> "$GITHUB_STEP_SUMMARY"
        fi

        if [[ "${COVERAGE}" == "true" && -n "${total_coverage:-}" ]]; then
            echo "" >> "$GITHUB_STEP_SUMMARY"
            echo "**Coverage:** ${total_coverage}%" >> "$GITHUB_STEP_SUMMARY"
        fi
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
        --ci)
            CI_MODE=true
            USE_GOTESTSUM=true
            # Auto-set JUnit output in CI if not specified
            if [[ -z "${JUNIT_OUTPUT}" ]]; then
                JUNIT_OUTPUT="test-results.xml"
            fi
            # Use more parallelism in CI
            if [[ "${PARALLEL_PACKAGES}" -eq 1 ]]; then
                PARALLEL_PACKAGES=4
            fi
            shift
            ;;
        --junit)
            JUNIT_OUTPUT="$2"
            shift 2
            ;;
        --gotestsum)
            USE_GOTESTSUM=true
            shift
            ;;
        --parallel)
            PARALLEL_PACKAGES="$2"
            shift 2
            ;;
        --no-color)
            RED=''
            GREEN=''
            YELLOW=''
            BLUE=''
            CYAN=''
            NC=''
            shift
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
if [[ "${WATCH}" == "true" ]]; then
    watch_tests
    exit 0
fi

# Run tests
run_tests "${TARGET}"
exit_code=$?

# Print CI summary
print_ci_summary

echo ""
if [ ${exit_code} -eq 0 ]; then
    success "All tests passed!"
else
    fail "Some tests failed."
    exit ${exit_code}
fi
