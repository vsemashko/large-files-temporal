#!/bin/bash

# Test script for large payload handling

set -e

echo "========================================="
echo "Testing Large Payload Handling"
echo "========================================="

# Check if services are running
if ! docker-compose ps | grep -q "Up"; then
    echo "Error: Services are not running. Start them with: docker-compose up -d"
    exit 1
fi

echo ""
echo "Services are running. Proceeding with tests..."

# Test with Go worker
echo ""
echo "========================================="
echo "Test 1: Go Worker with Large Payload"
echo "========================================="

cd samples/go-worker

echo "Building Go client..."
make build-client

echo ""
echo "Running Go client with large payload (5MB)..."
./client

cd ../..

# Test with TypeScript worker (if Node.js is available)
if command -v npm &> /dev/null; then
    echo ""
    echo "========================================="
    echo "Test 2: TypeScript Worker with Large Payload"
    echo "========================================="

    cd samples/typescript-worker

    if [ ! -d "node_modules" ]; then
        echo "Installing dependencies..."
        npm install
    fi

    echo ""
    echo "Running TypeScript client with large payload (5MB)..."
    npm run client

    cd ../..
else
    echo ""
    echo "Skipping TypeScript test (npm not installed)"
fi

echo ""
echo "========================================="
echo "Tests completed!"
echo "========================================="
echo ""
echo "Check the following:"
echo "  1. Temporal Web UI: http://localhost:8233"
echo "  2. Codec server logs: docker-compose logs codec-server"
echo "  3. Worker logs: docker-compose logs go-worker typescript-worker"
echo "  4. S3 objects (LocalStack): aws --endpoint-url=http://localhost:4566 s3 ls s3://temporal-large-payloads/ --recursive"
echo ""
