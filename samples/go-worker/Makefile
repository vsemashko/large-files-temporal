.PHONY: proto build-worker build-client run-worker run-client docker-build clean

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		pkg/proto/codec.proto

# Build the worker
build-worker: proto
	go build -o worker ./cmd/worker

# Build the client
build-client: proto
	go build -o client ./cmd/client

# Run the worker
run-worker: build-worker
	./worker

# Run the client
run-client: build-client
	./client

# Build Docker image
docker-build:
	docker build -t temporal-go-worker:latest .

# Clean build artifacts
clean:
	rm -f worker client
	rm -f pkg/proto/*.pb.go

# Install dependencies
deps:
	go mod download
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Run tests
test:
	go test -v ./...
