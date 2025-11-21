# Go Worker Sample

Sample Temporal worker in Go demonstrating large file processing with remote codec.

## Features

- Large file processing workflow
- Remote codec integration via gRPC
- Example activities for file processing
- Client for testing with 5MB payload

## Building

```bash
# Generate protobuf code
make proto

# Build worker
make build-worker

# Build client
make build-client
```

## Running

### Start Worker

```bash
export TEMPORAL_ADDRESS=localhost:7233
export CODEC_SERVER_URL=localhost:9090
make run-worker
```

### Run Client

```bash
make run-client
```

## Workflow

The `LargeFileProcessor` workflow:
1. Receives large file data (5MB+)
2. Processes the file (simulated)
3. Uploads to destination
4. Returns result with processing time

## Activities

- `ProcessLargeFile`: Simulates file processing
- `UploadToDestination`: Simulates upload to final destination

## Testing

```bash
make test
```

## Docker

```bash
make docker-build
docker run -e TEMPORAL_ADDRESS=temporal:7233 \
           -e CODEC_SERVER_URL=codec-server:9090 \
           temporal-go-worker:latest
```
