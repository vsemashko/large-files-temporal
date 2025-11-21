# Large Files Processing in Temporal

A complete solution for handling large payloads in Temporal workflows using a codec server that automatically stores oversized data in S3, with automatic cleanup for non-archived workflows.

## 🎯 Features

- **Codec Server (Go)**: Remote gRPC/HTTP server that intercepts Temporal payloads
- **Automatic S3 Storage**: Payloads exceeding 2MB threshold are automatically stored in S3
- **Transparent Retrieval**: S3-stored payloads are transparently retrieved when needed
- **Multi-Language Support**: Sample workers in Go and TypeScript
- **Automatic Cleanup**: Periodic cleanup of S3 objects for completed non-archived workflows
- **Docker Compose Setup**: Full stack with Temporal, LocalStack S3, and all components

## 📐 Architecture

```
┌─────────────┐         ┌──────────────┐         ┌──────────────┐
│   Worker    │◄───────►│ Codec Server │◄───────►│      S3      │
│ (Go/TS)     │  gRPC/  │   (Go)       │         │  (Storage)   │
└──────┬──────┘  HTTP   └──────────────┘         └──────────────┘
       │                        │
       │                        │
       ▼                        │
┌─────────────┐                 │
│  Temporal   │◄────────────────┘
│   Server    │
└─────────────┘
       ▲
       │
┌──────┴──────┐
│  Cleanup    │
│  Worker     │
└─────────────┘
```

## 🚀 Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.21+ (for local development)
- Node.js 20+ (for TypeScript samples)
- protoc (Protocol Buffers compiler)

### 1. Setup

```bash
# Clone the repository
git clone <repository-url>
cd large-files-temporal

# Run setup script
./scripts/setup.sh
```

### 2. Start Services

```bash
# Start all services (Temporal, Codec Server, Workers, LocalStack)
docker-compose up -d

# Check service status
docker-compose ps

# View logs
docker-compose logs -f
```

### 3. Test Large Payload Handling

```bash
# Run test script
./scripts/test-large-payload.sh
```

### 4. Access Temporal Web UI

Open http://localhost:8233 in your browser to view workflows.

## 📦 Components

### Codec Server

Go-based server that handles payload encoding/decoding:

- **Location**: `codec-server/`
- **Protocol**: gRPC (port 9090) and HTTP (port 8080)
- **Threshold**: 2MB (configurable)
- **Storage**: AWS S3 or LocalStack

```bash
cd codec-server
make build
make run
```

### Go Worker Sample

Sample Temporal worker in Go:

- **Location**: `samples/go-worker/`
- **Features**: Large file processing workflow with activities
- **Client**: Generates 5MB test file

```bash
cd samples/go-worker
make build-worker
make run-worker  # In one terminal
make run-client  # In another terminal
```

### TypeScript Worker Sample

Sample Temporal worker in TypeScript:

- **Location**: `samples/typescript-worker/`
- **Features**: Same workflow as Go worker
- **Client**: Generates 5MB test file

```bash
cd samples/typescript-worker
npm install
npm run dev  # In one terminal
npm run client  # In another terminal
```

### Cleanup Worker

Automated cleanup of S3 objects:

- **Location**: `cleanup-worker/`
- **Schedule**: Configurable (default: every 6 hours)
- **Grace Period**: 7 days (configurable)
- **Archive Check**: Skips archived workflows

```bash
cd cleanup-worker
make build
make run
```

## ⚙️ Configuration

Configuration is managed via environment variables. Copy `.env.example` to `.env` and customize:

```bash
# Payload Size Threshold
PAYLOAD_SIZE_THRESHOLD_BYTES=2097152  # 2MB

# S3 Configuration
S3_BUCKET=temporal-large-payloads
S3_REGION=us-east-1
S3_ENDPOINT=  # Empty for AWS, http://localhost:4566 for LocalStack

# Cleanup Configuration
CLEANUP_GRACE_PERIOD_DAYS=7
CHECK_ARCHIVE_BEFORE_DELETE=true
```

## 🔍 How It Works

### Encoding Flow

1. Worker sends payload to codec server
2. Codec server checks payload size
3. If > threshold:
   - Payload is uploaded to S3
   - S3 reference is created
   - Reference is sent to Temporal
4. If < threshold:
   - Payload is sent directly to Temporal

### Decoding Flow

1. Worker requests payload from codec server
2. Codec server checks if payload is S3 reference
3. If reference:
   - Payload is downloaded from S3
   - Original payload is returned to worker
4. If not reference:
   - Payload is returned as-is

### Cleanup Flow

1. Cleanup worker runs on schedule
2. Queries Temporal for completed workflows older than grace period
3. For each workflow:
   - Checks if workflow is archived (if enabled)
   - If not archived, deletes S3 objects for that workflow
4. Logs cleanup statistics

## 📊 S3 Object Structure

```
s3://temporal-large-payloads/
  ├── {namespace}/
  │   ├── {workflow-id}/
  │   │   ├── {run-id}/
  │   │   │   ├── {payload-hash-1}
  │   │   │   ├── {payload-hash-2}
  │   │   │   └── ...
```

### S3 Object Metadata

Each object includes metadata tags:
- `workflow-id`: Workflow identifier
- `run-id`: Workflow run identifier
- `namespace`: Temporal namespace
- `archived`: Boolean flag

## 🛠️ Development

### Building Components

```bash
# Codec Server
cd codec-server
make proto  # Generate protobuf code
make build  # Build binary
make test   # Run tests

# Go Worker
cd samples/go-worker
make proto
make build-worker
make build-client

# TypeScript Worker
cd samples/typescript-worker
npm install
npm run build

# Cleanup Worker
cd cleanup-worker
make build
```

### Running Locally

```bash
# Terminal 1: Start Temporal & LocalStack
docker-compose up temporal localstack

# Terminal 2: Start Codec Server
cd codec-server
export S3_ENDPOINT=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
make run

# Terminal 3: Start Worker
cd samples/go-worker
export CODEC_SERVER_URL=localhost:9090
make run-worker

# Terminal 4: Run Client
cd samples/go-worker
make run-client
```

## 🧪 Testing

### Test with LocalStack

```bash
# Check S3 buckets
aws --endpoint-url=http://localhost:4566 s3 ls

# List objects in bucket
aws --endpoint-url=http://localhost:4566 s3 ls s3://temporal-large-payloads/ --recursive

# Download an object
aws --endpoint-url=http://localhost:4566 s3 cp s3://temporal-large-payloads/{key} ./test-object
```

### Test Cleanup

```bash
# Trigger cleanup manually
cd cleanup-worker
# Set CLEANUP_GRACE_PERIOD_DAYS=0 to clean up immediately
export CLEANUP_GRACE_PERIOD_DAYS=0
make run
```

## 🚢 Production Deployment

### Security Best Practices

1. **IAM Roles**: Use IAM roles instead of access keys
2. **Encryption**: Enable S3 server-side encryption (SSE-S3 or SSE-KMS)
3. **Network**: Use VPC endpoints for S3 access
4. **TLS**: Enable TLS for gRPC connections
5. **Secrets**: Store credentials in AWS Secrets Manager or similar

### Kubernetes Deployment

```yaml
# Example deployment configuration
apiVersion: apps/v1
kind: Deployment
metadata:
  name: codec-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: codec-server
  template:
    metadata:
      labels:
        app: codec-server
    spec:
      serviceAccountName: codec-server-sa
      containers:
      - name: codec-server
        image: temporal-codec-server:latest
        env:
        - name: S3_BUCKET
          value: "temporal-large-payloads-prod"
        - name: PAYLOAD_SIZE_THRESHOLD_BYTES
          value: "2097152"
        ports:
        - containerPort: 9090
          name: grpc
        - containerPort: 8080
          name: http
```

### Monitoring

Key metrics to monitor:
- Payload size distribution
- S3 upload/download latency
- Codec server throughput
- Cleanup job success rate
- S3 storage costs

## 📝 Configuration Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `CODEC_GRPC_PORT` | gRPC server port | 9090 |
| `CODEC_HTTP_PORT` | HTTP server port | 8080 |
| `PAYLOAD_SIZE_THRESHOLD_BYTES` | Size threshold for S3 storage | 2097152 (2MB) |
| `S3_BUCKET` | S3 bucket name | temporal-large-payloads |
| `S3_REGION` | AWS region | us-east-1 |
| `S3_ENDPOINT` | Custom S3 endpoint (LocalStack/MinIO) | - |
| `CLEANUP_GRACE_PERIOD_DAYS` | Days before cleanup | 7 |
| `CHECK_ARCHIVE_BEFORE_DELETE` | Check archive before cleanup | true |
| `TEMPORAL_ADDRESS` | Temporal server address | localhost:7233 |

## 🤝 Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## 📄 License

[Your License Here]

## 🆘 Troubleshooting

### Codec Server Connection Issues

```bash
# Check codec server is running
docker-compose logs codec-server

# Test gRPC connection
grpcurl -plaintext localhost:9090 list
```

### S3 Upload Failures

```bash
# Check LocalStack is running
curl http://localhost:4566/_localstack/health

# Check S3 bucket exists
aws --endpoint-url=http://localhost:4566 s3 ls
```

### Worker Not Processing

```bash
# Check worker logs
docker-compose logs go-worker

# Check Temporal server
docker-compose logs temporal

# Verify worker is registered
# Check Temporal Web UI: http://localhost:8233
```

## 📚 Additional Resources

- [Temporal Documentation](https://docs.temporal.io/)
- [Temporal Go SDK](https://github.com/temporalio/sdk-go)
- [Temporal TypeScript SDK](https://github.com/temporalio/sdk-typescript)
- [AWS S3 Documentation](https://docs.aws.amazon.com/s3/)

## 🎯 Next Steps

- [ ] Add metrics and observability (Prometheus, Grafana)
- [ ] Implement payload compression
- [ ] Add support for multiple storage backends
- [ ] Implement webhook notifications for cleanup
- [ ] Add performance benchmarks
- [ ] Create Helm charts for Kubernetes deployment
