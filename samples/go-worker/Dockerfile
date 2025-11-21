# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install protobuf compiler
RUN apk add --no-cache protobuf-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate protobuf code
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pkg/proto/codec.proto

# Build the worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /worker ./cmd/worker

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /worker /app/worker

# Run the worker
CMD ["/app/worker"]
