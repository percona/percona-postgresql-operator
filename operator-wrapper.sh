#!/bin/bash

# Set ulimit before starting the operator
ulimit -n 1000000

# Start the operator
exec /usr/local/bin/postgres-operator "$@" 
