#!/bin/bash

# Setup script for large-files-temporal project

set -e

echo "========================================="
echo "Setting up Large Files Temporal Project"
echo "========================================="

# Check prerequisites
echo "Checking prerequisites..."

if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed"
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo "Error: Docker Compose is not installed"
    exit 1
fi

if ! command -v protoc &> /dev/null; then
    echo "Warning: protoc is not installed. Install it for local development."
    echo "  macOS: brew install protobuf"
    echo "  Ubuntu: apt-get install protobuf-compiler"
fi

echo "Prerequisites check passed!"

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo "Creating .env file from .env.example..."
    cp .env.example .env
    echo "Please review and update .env file if needed"
fi

# Generate protobuf code for local development
echo ""
echo "Generating protobuf code..."

if command -v protoc &> /dev/null; then
    # Codec server
    echo "  - codec-server"
    cd codec-server
    make proto 2>/dev/null || echo "    Skipping (will be generated in Docker)"
    cd ..

    # Go worker
    echo "  - go-worker"
    cd samples/go-worker
    make proto 2>/dev/null || echo "    Skipping (will be generated in Docker)"
    cd ../..
else
    echo "  Skipping (protoc not installed, will be generated in Docker)"
fi

# Pull Docker images
echo ""
echo "Pulling Docker images..."
docker-compose pull

# Build Docker images
echo ""
echo "Building Docker images..."
docker-compose build

echo ""
echo "========================================="
echo "Setup completed successfully!"
echo "========================================="
echo ""
echo "Next steps:"
echo "  1. Start the services:"
echo "     docker-compose up -d"
echo ""
echo "  2. Check the logs:"
echo "     docker-compose logs -f"
echo ""
echo "  3. Test with Go client:"
echo "     cd samples/go-worker"
echo "     make run-client"
echo ""
echo "  4. Test with TypeScript client:"
echo "     cd samples/typescript-worker"
echo "     npm install"
echo "     npm run client"
echo ""
echo "  5. Access Temporal Web UI:"
echo "     http://localhost:8233"
echo ""
echo "  6. Stop the services:"
echo "     docker-compose down"
echo ""
