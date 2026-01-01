#!/bin/bash
# Helper script to run HTTP API tests against AWS VMs
#
# Usage:
#   ./run-tests-aws.sh <server1-ip> <server2-ip> [server3-ip]
#
# Example:
#   ./run-tests-aws.sh 54.123.45.67 54.123.45.68 54.123.45.69

set -e

if [ -z "$1" ]; then
    echo "Error: Please provide at least one server IP"
    echo "Usage: $0 <server1-ip> <server2-ip> [server3-ip]"
    exit 1
fi

SERVER1_IP=$1
SERVER2_IP=${2:-$SERVER1_IP}
SERVER3_IP=${3:-$SERVER2_IP}

# Shift the first 3 arguments (the IPs) so remaining args can be passed to go test
shift 3 2>/dev/null || shift $# 2>/dev/null || true

# Export environment variables for the test framework
export CP_SERVER1_URL="http://${SERVER1_IP}:3000"
export CP_SERVER2_URL="http://${SERVER2_IP}:3000"
export CP_SERVER3_URL="http://${SERVER3_IP}:3000"

echo "========================================="
echo "Running tests against AWS VMs"
echo "========================================="
echo "Server 1: $CP_SERVER1_URL"
echo "Server 2: $CP_SERVER2_URL"
echo "Server 3: $CP_SERVER3_URL"
echo "========================================="
echo ""

# Debug: Print what Go will see
echo "Debug: Environment variables that will be passed to tests:"
echo "  CP_SERVER1_URL=$CP_SERVER1_URL"
echo "  CP_SERVER2_URL=$CP_SERVER2_URL"
echo "  CP_SERVER3_URL=$CP_SERVER3_URL"
echo ""

# Run the tests (pass any remaining args like -run TestName)
cd "$(dirname "$0")/.."
env | grep CP_SERVER  # Show env vars before running tests
go test -v -tags=http_apitest ./http-apitest/tests/... "$@"
