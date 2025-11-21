# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /cleanup-worker ./cmd/worker

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /cleanup-worker /app/cleanup-worker

# Run the worker
CMD ["/app/cleanup-worker"]
