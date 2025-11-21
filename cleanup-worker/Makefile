.PHONY: build run docker-build clean

# Build the worker
build:
	go build -o cleanup-worker ./cmd/worker

# Run the worker locally
run: build
	./cleanup-worker

# Build Docker image
docker-build:
	docker build -t temporal-cleanup-worker:latest .

# Clean build artifacts
clean:
	rm -f cleanup-worker

# Install dependencies
deps:
	go mod download

# Run tests
test:
	go test -v ./...
