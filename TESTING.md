# Testing Guide

This document describes how to run tests for the Percona PostgreSQL Operator locally using the Docker-based testing environment.

## Overview

The operator has several types of tests:

- **CI Tests**: Basic tests that run in CI/CD pipelines
- **Full Test Suite**: Complete test coverage including envtest
- **Specific Tests**: Individual test cases for debugging

All tests can be run locally using Docker to ensure a consistent testing environment.

## Quick Start

### Prerequisites

- Docker installed and running
- Bash shell (macOS/Linux)

### Basic Usage

```bash
# Run CI tests (default)
./test.sh

# Run all tests
./test.sh -m all

# Run a specific test
./test.sh -m specific -t TestReconcilePostgresClusterDataSource

# Show help
./test.sh -h
```

## Detailed Usage

### Test Modes

The test script supports three modes:

1. **All Mode** (`-m all`): Runs the complete test suite with envtest
2. **Specific Mode** (`-m specific`): Runs individual test cases

### Command Line Options

```bash
./test.sh [OPTIONS]

OPTIONS:
  -m, --mode MODE         Test mode: ci, all, or specific (default: ci)
  -t, --test TEST         Specific test to run (for specific mode)
  -p, --package PACKAGE   Specific package to test (default: ./internal/controller/postgrescluster)
  -v, --verbose          Enable verbose output
  -b, --build-only       Only build the Docker image, don't run tests
  -h, --help             Show help message
```

### Examples

```bash
# Run CI tests
./test.sh

# Run full test suite with verbose output
./test.sh -m all -v

# Run specific test in default package
./test.sh -m specific -t TestReconcilePostgresClusterDataSource

# Run specific test in custom package
./test.sh -m specific -t TestSomeFunction -p ./pkg/some/package

# Just build the test environment (useful for debugging)
./test.sh -b

# Run test with verbose output
./test.sh -m specific -t TestReconcilePostgresClusterDataSource -v
```

## Using Make

You can also run tests using Make targets:

```bash
# Run CI tests
make test-docker

# Run all tests
make test-docker TEST_MODE=all

# Run specific test
make test-docker TEST_MODE=specific TEST_NAME=TestReconcilePostgresClusterDataSource

# Run with verbose output
make test-docker TEST_MODE=specific TEST_NAME=TestReconcilePostgresClusterDataSource VERBOSE=1

# Run test in specific package
make test-docker TEST_MODE=specific TEST_NAME=TestSomeTest TEST_PACKAGE=./pkg/some/package
```

## Test Environment

The Docker test environment includes:

- Ubuntu latest base image
- Go 1.24.3
- All required dependencies (build tools, Git, curl, etc.)
- Pre-configured envtest with Kubernetes 1.32
- All necessary Go modules and tools

The environment is built from `Dockerfile.test` and provides:

- Consistent testing environment across different machines
- Isolated test execution
- All dependencies pre-installed
- Environment variables properly configured

## Debugging Failed Tests

When a test fails, you can:

1. **Run with verbose output**:
   ```bash
   ./test.sh -m specific -t TestFailingTest -v
   ```

2. **Build the environment and run interactively**:
   ```bash
   ./test.sh -b
   docker run --rm -it pgo-test bash
   ```

3. **Run the test manually inside the container**:
   ```bash
   source <(/workspace/hack/tools/setup-envtest --bin-dir=/workspace/hack/tools/envtest use 1.32 --print=env)
   PGO_NAMESPACE='postgres-operator' \
   QUERIES_CONFIG_DIR='/workspace/hack/tools/queries' \
   CGO_ENABLED=1 go test -v -count=1 -tags=envtest ./internal/controller/postgrescluster -run TestFailingTest
   ```

## Running Tests Natively (Without Docker)

If you prefer to run tests natively without Docker:

```bash
# Set up envtest
make tools/setup-envtest
make get-pgmonitor get-external-snapshotter

# Run basic tests
make check

# Run tests with envtest
make check-envtest

# Run specific test natively
source <(hack/tools/setup-envtest --bin-dir=hack/tools/envtest use 1.32 --print=env)
PGO_NAMESPACE='postgres-operator' \
QUERIES_CONFIG_DIR='hack/tools/queries' \
CGO_ENABLED=1 go test -v -count=1 -tags=envtest ./internal/controller/postgrescluster -run TestSpecificTest
```
