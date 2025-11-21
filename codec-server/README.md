# Codec Server

Go-based gRPC/HTTP server that handles encoding and decoding of Temporal payloads, automatically storing large payloads in S3.

## Features

- **Dual Protocol Support**: gRPC for Go workers, HTTP for TypeScript workers
- **Automatic S3 Storage**: Stores payloads exceeding configurable threshold
- **Transparent Retrieval**: Automatically retrieves S3-stored payloads
- **Configurable Threshold**: Default 2MB, adjust via environment variable
- **S3 Encryption**: Server-side encryption enabled by default

## Architecture

```
Worker → Codec Server → S3
         ↓
      Temporal
```

## Building

```bash
# Install dependencies
make deps

# Generate protobuf code
make proto

# Build binary
make build

# Run tests
make test
```

## Running

### Local Development

```bash
# With LocalStack
export S3_ENDPOINT=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export S3_BUCKET=temporal-large-payloads
make run
```

### Docker

```bash
make docker-build
make docker-run
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CODEC_GRPC_PORT` | gRPC server port | 9090 |
| `CODEC_HTTP_PORT` | HTTP server port | 8080 |
| `PAYLOAD_SIZE_THRESHOLD_BYTES` | Size threshold | 2097152 |
| `S3_BUCKET` | S3 bucket name | temporal-large-payloads |
| `S3_REGION` | AWS region | us-east-1 |
| `S3_ENDPOINT` | Custom endpoint | - |

## API

### gRPC

```protobuf
service PayloadCodec {
  rpc Encode(EncodeRequest) returns (EncodeResponse)
  rpc Decode(DecodeRequest) returns (DecodeResponse)
}
```

### HTTP

```bash
# Encode
POST /encode
Content-Type: application/json
{
  "payloads": [...],
  "namespace": "default",
  "workflowId": "wf-123",
  "runId": "run-456"
}

# Decode
POST /decode
Content-Type: application/json
{
  "payloads": [...]
}
```

## S3 Reference Format

```json
{
  "type": "s3-reference",
  "bucket": "temporal-large-payloads",
  "key": "namespace/workflow-id/run-id/payload-hash",
  "size": 10485760,
  "encoding": "json/protobuf",
  "hash": "sha256-hash"
}
```

## Monitoring

Key metrics:
- Payload size distribution
- S3 upload/download latency
- Encoding/decoding throughput
- Error rates
