#!/usr/bin/env bash
#
# Orchestra — Multi-Agent AI Orchestration Engine
# Lint Script
#
# Usage:
#   ./scripts/lint.sh                # Run all linters
#   ./scripts/lint.sh -v             # Verbose output
#   ./scripts/lint.sh --fix          # Auto-fix issues where possible
#   ./scripts/lint.sh -f             # Check formatting only
#   ./scripts/lint.sh -p message     # Lint specific package
#   ./scripts/lint.sh --no-install   # Skip linter installation check

set -euo pipefail

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default settings
VERBOSE=false
FIX=false
FORMAT_ONLY=false
NO_INSTALL=false
SPECIFIC_PKG=""
TARGET="./..."
LINTER="golangci-lint"
LINTER_VERSION="v1.64"
TIMEOUT="5m"

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
    echo -e "${GREEN}[OK]${NC} $1"
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
Orchestra Lint Script

Usage:
  $(basename "$0") [options]

Options:
  -v, --verbose         Verbose output
  -f, --format          Check formatting only (gofmt, goimports)
      --fix             Auto-fix issues where possible
      --no-install      Skip linter installation check
  -p, --pkg string      Lint a specific package (e.g., "message", "provider")
  -t, --timeout string  Linter timeout duration (default: "5m")
  -h, --help            Show this help message

Packages:
  message     internal/message
  provider    internal/provider
  config      internal/config
  mock        internal/provider/mock
  all         ./... (default)

Linters:
  - gofmt        (formatting)
  - goimports    (import ordering)
  - go vet       (static analysis)
  - golangci-lint (comprehensive linting suite)

Examples:
  $(basename "$0")                       # Run all linters
  $(basename "$0") -v                    # Verbose output
  $(basename "$0") --fix                 # Auto-fix issues
  $(basename "$0") -f                    # Check formatting only
  $(basename "$0") -p provider           # Lint provider package only
  $(basename "$0") -p config -v          # Lint config package with verbose output
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
        cmd)
            echo "./cmd/..."
            ;;
        pkg)
            echo "./pkg/..."
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

# ---------------------------------------------------------------------------
# Lint Steps
# ---------------------------------------------------------------------------

check_go_version() {
    if ! command -v go &>/dev/null; then
        error "Go is not installed. Please install Go 1.24+ and try again."
    fi

    local go_version
    go_version=$(go version | awk '{print $3}')
    info "Using ${go_version}"
}

check_formatting() {
    info "Checking Go formatting (gofmt)..."

    local unformatted
    unformatted=$(gofmt -l "${PROJECT_ROOT}/cmd" "${PROJECT_ROOT}/internal" "${PROJECT_ROOT}/pkg" 2>/dev/null || true)

    if [ -n "${unformatted}" ]; then
        fail "The following files are not properly formatted:"
        echo ""
        echo "${unformatted}" | while read -r file; do
            echo "  ${file}"
        done
        echo ""

        if [ "${FIX}" = true ]; then
            info "Auto-fixing formatting..."
            gofmt -w "${PROJECT_ROOT}/cmd" "${PROJECT_ROOT}/internal" "${PROJECT_ROOT}/pkg"
            success "Formatting fixed."
        else
            fail "Run 'gofmt -w .' or './scripts/lint.sh --fix' to fix."
            return 1
        fi
    else
        success "All files are properly formatted."
    fi
}

check_goimports() {
    info "Checking import ordering (goimports)..."

    if ! command -v goimports &>/dev/null; then
        if [ "${NO_INSTALL}" = false ]; then
            info "Installing goimports..."
            go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null || true
        fi

        if ! command -v goimports &>/dev/null; then
            warn "goimports not found, skipping import check."
            return 0
        fi
    fi

    local unformatted
    unformatted=$(goimports -l \
        -local "github.com/user/orchestra" \
        "${PROJECT_ROOT}/cmd" \
        "${PROJECT_ROOT}/internal" \
        "${PROJECT_ROOT}/pkg" 2>/dev/null || true)

    if [ -n "${unformatted}" ]; then
        fail "The following files have import ordering issues:"
        echo ""
        echo "${unformatted}" | while read -r file; do
            echo "  ${file}"
        done
        echo ""

        if [ "${FIX}" = true ]; then
            info "Auto-fixing import ordering..."
            goimports -w \
                -local "github.com/user/orchestra" \
                "${PROJECT_ROOT}/cmd" \
                "${PROJECT_ROOT}/internal" \
                "${PROJECT_ROOT}/pkg"
            success "Import ordering fixed."
        else
            fail "Run 'goimports -w -local github.com/user/orchestra .' or './scripts/lint.sh --fix' to fix."
            return 1
        fi
    else
        success "All imports are properly ordered."
    fi
}

run_go_vet() {
    info "Running go vet..."

    cd "${PROJECT_ROOT}"

    local vet_output
    vet_output=$(go vet "${TARGET}" 2>&1) || true

    if [ -n "${vet_output}" ]; then
        fail "go vet found issues:"
        echo ""
        echo "${vet_output}" | while read -r line; do
            echo "  ${line}"
        done
        echo ""
        return 1
    else
        success "go vet passed."
    fi
}

run_golangci_lint() {
    info "Running golangci-lint..."

    # Check if golangci-lint is installed
    if ! command -v "${LINTER}" &>/dev/null; then
        if [ "${NO_INSTALL}" = false ]; then
            info "Installing golangci-lint ${LINTER_VERSION}..."
            go install "github.com/golangci/golangci-lint/cmd/golangci-lint@${LINTER_VERSION}" 2>/dev/null || true
        fi

        if ! command -v "${LINTER}" &>/dev/null; then
            error "golangci-lint is not installed.\nInstall it with:\n  go install github.com/golangci/golangci-lint/cmd/golangci-lint@${LINTER_VERSION}\n  Or visit: https://golangci-lint.run/usage/install/"
        fi
    fi

    cd "${PROJECT_ROOT}"

    local linter_version
    linter_version=$(${LINTER} version 2>/dev/null | head -1 || echo "unknown")
    if [ "${VERBOSE}" = true ]; then
        info "Linter version: ${linter_version}"
    fi

    # Build the command
    local cmd="${LINTER} run"
    cmd="${cmd} --timeout=${TIMEOUT}"

    if [ "${VERBOSE}" = true ]; then
        cmd="${cmd} -v"
    fi

    if [ "${FIX}" = true ]; then
        cmd="${cmd} --fix"
    fi

    # If a specific package is requested, lint only that package
    if [ -n "${SPECIFIC_PKG}" ]; then
        local resolved
        resolved=$(resolve_package "${SPECIFIC_PKG}")
        # Remove the trailing /... for linting a specific directory
        resolved="${resolved%/...}"
        cmd="${cmd} ${resolved}"
    else
        cmd="${cmd} ./..."
    fi

    local exit_code=0
    eval "${cmd}" || exit_code=$?

    if [ ${exit_code} -eq 0 ]; then
        success "golangci-lint passed."
    else
        fail "golangci-lint found issues (exit code: ${exit_code})."
        return ${exit_code}
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
        -f|--format)
            FORMAT_ONLY=true
            shift
            ;;
        --fix)
            FIX=true
            shift
            ;;
        --no-install)
            NO_INSTALL=true
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

echo -e "${CYAN}Orchestra — Lint Suite${NC}"
echo -e "${CYAN}=======================${NC}"
echo ""

cd "${PROJECT_ROOT}"

check_go_version

# Resolve target package
TARGET=$(resolve_package "${SPECIFIC_PKG}")

if [ "${VERBOSE}" = true ]; then
    info "Target: ${TARGET}"
    info "Fix mode: ${FIX}"
    info "Format only: ${FORMAT_ONLY}"
    echo ""
fi

# Track overall status
OVERALL_EXIT=0

# Step 1: Formatting
check_formatting || OVERALL_EXIT=1

if [ "${FORMAT_ONLY}" = true ]; then
    echo ""
    if [ ${OVERALL_EXIT} -eq 0 ]; then
        success "Formatting check passed!"
    else
        fail "Formatting check failed."
    fi
    exit ${OVERALL_EXIT}
fi

# Step 2: Import ordering
check_goimports || OVERALL_EXIT=1

# Step 3: Go vet
run_go_vet || OVERALL_EXIT=1

# Step 4: golangci-lint
run_golangci_lint || OVERALL_EXIT=1

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
echo -e "${CYAN}=======================${NC}"

if [ ${OVERALL_EXIT} -eq 0 ]; then
    success "All lint checks passed!"
else
    fail "Some lint checks failed."
    if [ "${FIX}" = false ]; then
        echo ""
        info "Tip: Run with --fix to auto-fix formatting and import issues:"
        info "  ./scripts/lint.sh --fix"
    fi
fi

exit ${OVERALL_EXIT}
