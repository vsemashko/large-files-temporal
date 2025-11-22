# Quick Start Guide

Get your Temporal Large Files Codec Server up and running in minutes.

## Prerequisites

### Required Tools

Install using [mise](https://mise.jdx.dev/) (recommended):

```bash
# Install mise
curl https://mise.run | sh

# Install all project tools
mise install

# Verify installations
go version        # Go 1.21+
node --version    # Node.js 20+
kubectl version   # kubectl 1.31+
k6 version        # k6 0.54+
```

### Manual Installation

If not using mise, install manually:
- Go 1.21+ - https://golang.org/dl/
- Node.js 20+ - https://nodejs.org/
- Docker - https://docs.docker.com/get-docker/
- kubectl - https://kubernetes.io/docs/tasks/tools/
- k6 (optional) - https://k6.io/docs/get-started/installation/

## Local Development

### 1. Start LocalStack (S3)

```bash
# Start local S3
docker-compose up -d localstack

# Verify S3 is running
aws --endpoint-url=http://localhost:4566 s3 ls
```

### 2. Build and Run Codec Server

```bash
cd codec-server

# Generate protobuf code (requires protoc)
make proto

# Run tests
go test ./...

# Build
go build -o codec-server ./cmd/server

# Run locally
export S3_ENDPOINT=http://localhost:4566
export S3_BUCKET=temporal-large-payloads
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_REGION=us-east-1

./codec-server
```

Server will start on:
- gRPC: `localhost:9090`
- HTTP: `localhost:8080`
- Metrics: `localhost:8080/metrics`
- Health: `localhost:8080/health`

### 3. Test the Server

```bash
# Check health
curl http://localhost:8080/health

# Run integration test
./scripts/test-large-payload.sh
```

## Kubernetes Deployment

### Option 1: Using Helm (Recommended)

```bash
# Development deployment
helm install codec-server ./helm/codec-server \
  --namespace temporal \
  --create-namespace \
  --set config.s3.bucket=your-s3-bucket \
  --set config.s3.region=us-east-1

# Verify deployment
kubectl get pods -n temporal
kubectl logs -n temporal -l app=codec-server
```

### Option 2: Using kubectl

```bash
# Edit k8s/codec-server-deployment.yaml with your S3 bucket
vim k8s/codec-server-deployment.yaml

# Deploy
kubectl apply -f k8s/codec-server-deployment.yaml

# Verify
kubectl get deployment codec-server -n temporal
```

### Option 3: Using Deployment Script

```bash
# Development environment
ENVIRONMENT=dev ./scripts/deploy-codec-server.sh

# Staging environment (requires TLS and API key secrets)
ENVIRONMENT=staging ./scripts/deploy-codec-server.sh

# Production environment
ENVIRONMENT=prod ./scripts/deploy-codec-server.sh
```

## Configure Workers

### Go Worker

```go
import (
    "github.com/vsemashko/large-files-temporal/samples/go-worker/internal/codec"
)

// Create remote codec client
remoteCodec, err := codec.NewRemotePayloadCodec("localhost:9090", false, "")
if err != nil {
    panic(err)
}

// Configure worker with codec
workerOptions := worker.Options{
    DataConverter: temporal.NewCodecDataConverter(
        temporal.GetDefaultDataConverter(),
        remoteCodec,
    ),
}
```

### TypeScript Worker

```typescript
import { RemoteCodec } from './codec/remote-codec';

const codec = new RemoteCodec('http://localhost:8080');

const worker = await Worker.create({
  dataConverter: {
    payloadCodec: codec,
  },
  // ... other options
});
```

## Production Checklist

Before deploying to production:

- [ ] **S3 Bucket Created** with versioning enabled
- [ ] **IAM Role** configured with S3 read/write permissions
- [ ] **TLS Certificates** created and deployed as secrets
- [ ] **API Keys** generated and deployed as secrets
- [ ] **Monitoring** deployed (Prometheus + Grafana)
- [ ] **Alerting** configured (AlertManager)
- [ ] **Load Tests** run successfully
- [ ] **Backup Strategy** documented
- [ ] **Runbooks** created for common issues

### Deploy Monitoring

```bash
# Deploy Prometheus alerts
./scripts/deploy-monitoring.sh

# Import Grafana dashboard
# Upload monitoring/grafana-dashboard-codec-server.json via Grafana UI
```

### Run Load Tests

```bash
# Against local server
CODEC_SERVER_URL=http://localhost:8080 k6 run tests/load-test.js

# Against Kubernetes service
kubectl port-forward -n temporal svc/codec-server 8080:8080 &
CODEC_SERVER_URL=http://localhost:8080 k6 run tests/load-test.js
```

## Verify Everything Works

### 1. Health Check

```bash
curl http://codec-server:8080/health | jq
```

Expected output:
```json
{
  "status": "healthy",
  "checks": {
    "s3": "ok",
    "codec": "ok"
  },
  "version": "1.0.0"
}
```

### 2. Metrics Check

```bash
curl http://codec-server:8080/metrics | grep codec_
```

Should see metrics like:
```
codec_encode_requests_total 0
codec_decode_requests_total 0
s3_uploads_total 0
...
```

### 3. End-to-End Test

```bash
# Encode a large payload
curl -X POST http://codec-server:8080/encode \
  -H "Content-Type: application/json" \
  -d '{
    "payloads": [{
      "metadata": {"encoding": "json/plain"},
      "data": "'$(dd if=/dev/urandom bs=1M count=3 2>/dev/null | base64 -w 0)'"
    }],
    "namespace": "test",
    "workflowId": "test-workflow",
    "runId": "test-run"
  }' | jq

# Verify S3 upload
aws s3 ls s3://your-bucket/test/test-workflow/test-run/
```

## Troubleshooting

### Server won't start

```bash
# Check logs
kubectl logs -n temporal -l app=codec-server

# Common issues:
# - S3 bucket doesn't exist
# - AWS credentials not configured
# - Port already in use
```

### High latency

```bash
# Check S3 latency
aws s3 ls s3://your-bucket/ --debug

# Check server metrics
curl http://codec-server:8080/metrics | grep duration

# Run load test to identify bottleneck
k6 run tests/load-test.js
```

### Connection refused

```bash
# Check if server is running
kubectl get pods -n temporal -l app=codec-server

# Check service
kubectl get svc -n temporal codec-server

# Port forward to test locally
kubectl port-forward -n temporal svc/codec-server 8080:8080
curl http://localhost:8080/health
```

## Next Steps

1. **Read Full Documentation**
   - [Production Readiness](PRODUCTION_READINESS_COMPLETE.md)
   - [Testing Guide](TESTING.md)
   - [Monitoring Setup](monitoring/README.md)
   - [Remaining Work Plan](REMAINING_WORK_PLAN.md)

2. **Configure CI/CD**
   - GitHub Actions workflow is ready in `.github/workflows/ci.yml`
   - Add `CODECOV_TOKEN` secret for coverage reporting
   - Push to main branch to trigger builds

3. **Set Up Monitoring**
   - Deploy Prometheus alert rules
   - Import Grafana dashboard
   - Configure AlertManager routing

4. **Run Load Tests**
   - Establish performance baselines
   - Add to CI/CD for regression testing

5. **Deploy Cleanup Worker**
   - Prevents S3 bucket from growing indefinitely
   - See `cleanup-worker/README.md`

## Getting Help

- **Documentation**: See `/docs` directory
- **Issues**: https://github.com/vsemashko/large-files-temporal/issues
- **Runbooks**: `docs/runbooks/` (create as needed)

## Common Commands Cheat Sheet

```bash
# Local development
make proto                    # Generate protobuf code
go test ./...                 # Run tests
go build ./cmd/server         # Build binary

# Kubernetes deployment
helm install codec-server ./helm/codec-server
kubectl get pods -n temporal
kubectl logs -f -l app=codec-server

# Monitoring
kubectl port-forward svc/prometheus 9090:9090
kubectl port-forward svc/grafana 3000:3000

# Load testing
k6 run tests/load-test.js
k6 run --vus 100 --duration 5m tests/load-test.js

# Cleanup
helm uninstall codec-server
kubectl delete namespace temporal
docker-compose down
```
