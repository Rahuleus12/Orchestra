#!/usr/bin/env bash
#
# Orchestra — Multi-Agent AI Orchestration Engine
# Build Script
#
# Usage:
#   ./scripts/build.sh              # Build for current platform
#   ./scripts/build.sh -o linux     # Build for Linux
#   ./scripts/build.sh -o windows   # Build for Windows
#   ./scripts/build.sh -o darwin    # Build for macOS
#   ./scripts/build.sh -a amd64    # Build for amd64
#   ./scripts/build.sh -a arm64    # Build for arm64
#   ./scripts/build.sh --all       # Build for all platforms
#   ./scripts/build.sh -c          # Build with coverage instrumentation

set -euo pipefail

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BINARY_NAME="orchestra"
BUILD_DIR="${PROJECT_ROOT}/bin"
MAIN_PKG="./cmd/orchestra"

# Default build settings
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
CGO_ENABLED=0

# Version information
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
BUILD_DATE="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# Build flags
LDFLAGS="-s -w"
LDFLAGS="${LDFLAGS} -X main.Version=${VERSION}"
LDFLAGS="${LDFLAGS} -X main.GitCommit=${GIT_COMMIT}"
LDFLAGS="${LDFLAGS} -X main.BuildDate=${BUILD_DATE}"

# Options
BUILD_ALL=false
VERBOSE=false
COVERAGE=false

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

usage() {
    cat <<EOF
Orchestra Build Script

Usage:
  $(basename "$0") [options]

Options:
  -o, --os string       Target OS (linux, windows, darwin). Default: current OS
  -a, --arch string     Target architecture (amd64, arm64). Default: current arch
  -b, --binary string   Output binary name. Default: orchestra
  -c, --coverage        Build with coverage instrumentation
      --all             Build for all supported platforms
  -v, --verbose         Verbose output
  -h, --help            Show this help message

Examples:
  $(basename "$0")                       # Build for current platform
  $(basename "$0") --all                 # Build all platforms
  $(basename "$0") -o linux -a arm64     # Cross-compile for Linux ARM64
  $(basename "$0") -c                    # Build with coverage
EOF
}

build_binary() {
    local target_os="$1"
    local target_arch="$2"
    local output_name="${BINARY_NAME}"

    if [ "${target_os}" = "windows" ]; then
        output_name="${output_name}.exe"
    fi

    if [ "${BUILD_ALL}" = true ]; then
        output_name="${BINARY_NAME}-${target_os}-${target_arch}"
        if [ "${target_os}" = "windows" ]; then
            output_name="${output_name}.exe"
        fi
    fi

    local output_path="${BUILD_DIR}/${output_name}"

    if [ "${VERBOSE}" = true ]; then
        info "Building ${output_name} (GOOS=${target_os}, GOARCH=${target_arch})"
        info "  Version:    ${VERSION}"
        info "  Git Commit: ${GIT_COMMIT}"
        info "  Build Date: ${BUILD_DATE}"
        info "  Output:     ${output_path}"
    fi

    local build_flags="-ldflags=\"${LDFLAGS}\""
    if [ "${COVERAGE}" = true ]; then
        build_flags="${build_flags} -cover"
    fi

    cd "${PROJECT_ROOT}"

    CGO_ENABLED=${CGO_ENABLED} GOOS=${target_os} GOARCH=${target_arch} \
        go build \
        -trimpath \
        -ldflags="${LDFLAGS}" \
        $(if [ "${COVERAGE}" = true ]; then echo "-cover"; fi) \
        -o "${output_path}" \
        ${MAIN_PKG}

    if [ $? -eq 0 ]; then
        local size
        size=$(du -h "${output_path}" | cut -f1 | tr -d ' ')
        success "Built ${output_name} (${size})"
    else
        error "Failed to build ${output_name}"
    fi
}

# ---------------------------------------------------------------------------
# Parse Arguments
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--os)
            GOOS="$2"
            shift 2
            ;;
        -a|--arch)
            GOARCH="$2"
            shift 2
            ;;
        -b|--binary)
            BINARY_NAME="$2"
            shift 2
            ;;
        -c|--coverage)
            COVERAGE=true
            shift
            ;;
        --all)
            BUILD_ALL=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
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

info "Orchestra Build Script"
info "========================"

# Create build directory
mkdir -p "${BUILD_DIR}"

# Check Go installation
if ! command -v go &>/dev/null; then
    error "Go is not installed. Please install Go 1.24+ and try again."
fi

go_version=$(go version | awk '{print $3}')
info "Using ${go_version}"

# Download dependencies
if [ "${VERBOSE}" = true ]; then
    info "Downloading dependencies..."
fi
cd "${PROJECT_ROOT}" && go mod download

# Build
if [ "${BUILD_ALL}" = true ]; then
    info "Building for all platforms..."

    PLATFORMS=(
        "linux/amd64"
        "linux/arm64"
        "darwin/amd64"
        "darwin/arm64"
        "windows/amd64"
        "windows/arm64"
    )

    for platform in "${PLATFORMS[@]}"; do
        IFS='/' read -r os arch <<< "${platform}"
        build_binary "${os}" "${arch}"
    done
else
    build_binary "${GOOS}" "${GOARCH}"
fi

echo ""
success "Build complete!"
info "Output directory: ${BUILD_DIR}"

if [ "${VERBOSE}" = true ]; then
    ls -lh "${BUILD_DIR}/"
fi
