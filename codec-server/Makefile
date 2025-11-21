.PHONY: proto build run docker-build docker-run clean

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		pkg/proto/codec.proto

# Build the server
build: proto
	go build -o codec-server ./cmd/server

# Run the server locally
run: build
	./codec-server

# Build Docker image
docker-build:
	docker build -t temporal-codec-server:latest .

# Run Docker container
docker-run:
	docker run -p 9090:9090 \
		-e S3_ENDPOINT=http://host.docker.internal:4566 \
		-e S3_BUCKET=temporal-large-payloads \
		-e AWS_ACCESS_KEY_ID=test \
		-e AWS_SECRET_ACCESS_KEY=test \
		temporal-codec-server:latest

# Clean build artifacts
clean:
	rm -f codec-server
	rm -f pkg/proto/*.pb.go

# Install dependencies
deps:
	go mod download
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Run tests
test:
	go test -v ./...
