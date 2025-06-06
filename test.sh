#!/bin/bash

# Wrapper script to run the consolidated test script
exec "$(dirname "$0")/hack/test-docker.sh" "$@"
