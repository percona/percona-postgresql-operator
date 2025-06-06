#!/bin/bash

set -e

# Default values
TEST_MODE="all"
SPECIFIC_TEST=""
SPECIFIC_PACKAGE=""
VERBOSE=""
BUILD_ONLY=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Run tests in Docker environment"
    echo ""
    echo "OPTIONS:"
    echo "  -m, --mode MODE         Test mode: all, or specific (default: all)"
    echo "  -t, --test TEST         Specific test to run (for specific mode)"
    echo "  -p, --package PACKAGE   Specific package to test (default: ./internal/controller/postgrescluster)"
    echo "  -v, --verbose          Enable verbose output"
    echo "  -b, --build-only       Only build the Docker image, don't run tests"
    echo "  -h, --help             Show this help message"
    echo ""
    echo "EXAMPLES:"
    echo "  $0                                           # Run all tests"
    echo "  $0 -m specific -t TestReconcilePostgresClusterDataSource"
    echo "  $0 -m specific -t TestSomeOtherTest -p ./pkg/some/package"
    echo "  $0 -b                                        # Just build the test image"
    echo ""
}

log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -m|--mode)
            TEST_MODE="$2"
            shift 2
            ;;
        -t|--test)
            SPECIFIC_TEST="$2"
            shift 2
            ;;
        -p|--package)
            SPECIFIC_PACKAGE="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE="-v"
            shift
            ;;
        -b|--build-only)
            BUILD_ONLY=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate test mode
if [[ ! "$TEST_MODE" =~ ^(ci|all|specific)$ ]]; then
    error "Invalid test mode: $TEST_MODE. Must be 'ci', 'all', or 'specific'"
    exit 1
fi

# Validate specific test requirements
if [[ "$TEST_MODE" == "specific" && -z "$SPECIFIC_TEST" ]]; then
    error "Specific test name is required when using 'specific' mode"
    echo "Use -t or --test to specify the test name"
    exit 1
fi

# Set default package for specific tests
if [[ "$TEST_MODE" == "specific" && -z "$SPECIFIC_PACKAGE" ]]; then
    SPECIFIC_PACKAGE="./internal/controller/postgrescluster"
fi

# Build Docker image
log "Building Docker test environment..."
if ! docker build -t pgo-test -f Dockerfile.test .; then
    error "Failed to build Docker test environment"
    exit 1
fi

success "Docker test environment built successfully"

# Exit if build-only mode
if [[ "$BUILD_ONLY" == true ]]; then
    success "Build completed. Use '$0 -m <mode>' to run tests."
    exit 0
fi

# Run tests based on mode
case $TEST_MODE in
    "ci" | "all")
        log "Running CI tests in Docker..."
        docker run --rm -it pgo-test bash -c "
        source <(/workspace/hack/tools/setup-envtest --bin-dir=/workspace/hack/tools/envtest use 1.32 --print=env) && \
        PGO_NAMESPACE='postgres-operator' \
        QUERIES_CONFIG_DIR='/workspace/hack/tools/queries' \
        make check
        make check-envtest
        "
        ;;
    "specific")
        log "Running specific test: $SPECIFIC_TEST in package: $SPECIFIC_PACKAGE"
        docker run --rm -it pgo-test bash -c "
        source <(/workspace/hack/tools/setup-envtest --bin-dir=/workspace/hack/tools/envtest use 1.32 --print=env) && \
        PGO_NAMESPACE='postgres-operator' \
        QUERIES_CONFIG_DIR='/workspace/hack/tools/queries' \
        CGO_ENABLED=1 go test $VERBOSE -count=1 -tags=envtest \
        $SPECIFIC_PACKAGE \
        -run $SPECIFIC_TEST
        "
        ;;
esac

if [[ $? -eq 0 ]]; then
    success "Tests completed successfully!"
else
    error "Tests failed!"
    exit 1
fi
