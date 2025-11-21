# Testing Guide

Comprehensive guide for testing the large files processing feature in Temporal.

## Table of Contents

- [Local Testing](#local-testing)
- [Integration Testing](#integration-testing)
- [Performance Testing](#performance-testing)
- [Production Validation](#production-validation)
- [Troubleshooting](#troubleshooting)

## Local Testing

### Prerequisites

- Docker and Docker Compose
- Go 1.21+
- Node.js 20+
- AWS CLI (for S3 verification)

### 1. Start Services

```bash
# Start all services
docker-compose up -d

# Wait for services to be healthy
docker-compose ps

# Check logs
docker-compose logs -f codec-server
```

### 2. Verify Codec Server

```bash
# Test gRPC endpoint (requires grpcurl)
grpcurl -plaintext localhost:9090 list

# Test HTTP health endpoint
curl http://localhost:8080/health

# Test metrics endpoint
curl http://localhost:8080/metrics
```

### 3. Test with Go Worker

```bash
cd samples/go-worker

# Terminal 1: Start worker
export TEMPORAL_ADDRESS=localhost:7233
export CODEC_SERVER_URL=localhost:9090
make run-worker

# Terminal 2: Run client
make run-client
```

**Expected Output:**
```
Generated test file: 5242880 bytes (5.00 MB)
Starting workflow...
Started workflow: WorkflowID=large-file-workflow-xxx, RunID=xxx
Workflow completed successfully!
  Processed Size: 5242880 bytes (5.00 MB)
  Upload URL: https://destination.example.com/test-large-file.bin-xxx
  Processing Time: 4.xxxs
```

### 4. Test with TypeScript Worker

```bash
cd samples/typescript-worker

# Install dependencies
npm install

# Terminal 1: Start worker
export TEMPORAL_ADDRESS=localhost:7233
export CODEC_SERVER_URL=http://localhost:8080
npm run dev

# Terminal 2: Run client
npm run client
```

### 5. Verify S3 Storage

```bash
# List S3 bucket (LocalStack)
aws --endpoint-url=http://localhost:4566 s3 ls s3://temporal-large-payloads/

# List objects recursively
aws --endpoint-url=http://localhost:4566 s3 ls s3://temporal-large-payloads/ --recursive

# Download an object
aws --endpoint-url=http://localhost:4566 s3 cp \
  s3://temporal-large-payloads/default/large-file-workflow-xxx/run-xxx/hash-xxx \
  ./test-object

# Verify size
ls -lh ./test-object
```

### 6. Test Cleanup Worker

```bash
# Run cleanup with 0 grace period (immediate cleanup)
docker-compose run --rm \
  -e CLEANUP_GRACE_PERIOD_DAYS=0 \
  cleanup-worker

# Verify objects were deleted
aws --endpoint-url=http://localhost:4566 s3 ls s3://temporal-large-payloads/ --recursive
```

## Integration Testing

### Test Scenarios

#### Scenario 1: Small Payload (< 2MB)

```bash
# Modify client to use small payload
# In go-worker/cmd/client/main.go change:
fileSize := 1 * 1024 * 1024  # 1MB

# Run client
make run-client
```

**Expected:** Payload stored inline, no S3 upload

#### Scenario 2: Large Payload (> 2MB)

```bash
# Use default 5MB payload
make run-client
```

**Expected:**
- Codec server logs show S3 upload
- S3 object created
- Workflow completes successfully

#### Scenario 3: Multiple Concurrent Workflows

```bash
# Run multiple clients in parallel
for i in {1..10}; do
  ./client &
done
wait
```

**Expected:**
- All workflows complete
- 10 S3 objects created
- No race conditions

#### Scenario 4: Workflow Failure

```bash
# Modify activity to fail
# In activities/file_processing.go:
return nil, errors.New("simulated failure")

# Run client
make run-client
```

**Expected:**
- S3 object created
- S3 object retained for debugging
- Cleanup worker can clean up later

#### Scenario 5: Archive and Cleanup

```bash
# 1. Run workflow
make run-client

# 2. Archive workflow (if archival enabled)
# Check Temporal UI: http://localhost:8233

# 3. Run cleanup
docker-compose run --rm cleanup-worker

# 4. Verify archived workflow's S3 objects NOT deleted
```

### Automated Integration Tests

Create `tests/integration_test.sh`:

```bash
#!/bin/bash
set -e

echo "Running integration tests..."

# Test 1: Small payload
echo "Test 1: Small payload (1MB)"
# ... test logic

# Test 2: Large payload
echo "Test 2: Large payload (5MB)"
# ... test logic

# Test 3: Concurrent workflows
echo "Test 3: Concurrent workflows"
# ... test logic

# Test 4: Cleanup
echo "Test 4: Cleanup"
# ... test logic

echo "All tests passed!"
```

## Performance Testing

### Load Testing

#### Tool: k6

Create `tests/load-test.js`:

```javascript
import http from 'k6/http';
import { check } from 'k6';

export let options = {
  stages: [
    { duration: '2m', target: 10 },  // Ramp up
    { duration: '5m', target: 10 },  // Steady state
    { duration: '2m', target: 0 },   // Ramp down
  ],
};

export default function () {
  const payload = {
    payloads: [
      {
        metadata: { encoding: 'json/plain' },
        data: btoa('x'.repeat(3 * 1024 * 1024)),  // 3MB
      },
    ],
    namespace: 'default',
    workflowId: `wf-${__VU}-${__ITER}`,
    runId: `run-${__VU}-${__ITER}`,
  };

  const res = http.post('http://localhost:8080/encode', JSON.stringify(payload), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(res, {
    'status is 200': (r) => r.status === 200,
    'response time < 500ms': (r) => r.timings.duration < 500,
  });
}
```

Run:
```bash
k6 run tests/load-test.js
```

#### Expected Results

| Metric | Target | Acceptable |
|--------|--------|------------|
| Encode latency (p95) | < 200ms | < 500ms |
| Encode latency (p99) | < 500ms | < 1s |
| S3 upload latency | < 1s | < 3s |
| Error rate | < 0.1% | < 1% |
| Throughput | > 100 RPS | > 50 RPS |

### Stress Testing

```bash
# Increase load gradually
k6 run --vus 100 --duration 10m tests/load-test.js
```

Monitor:
- CPU and memory usage
- S3 operation latency
- Error rates
- Queue depths

### Benchmark Tests

Create `codec-server/internal/codec/codec_test.go`:

```go
func BenchmarkEncode(b *testing.B) {
    // Setup
    storage := &mockStorage{}
    codec := NewCodec(storage, 2*1024*1024, "test-bucket")

    payload := createLargePayload(5 * 1024 * 1024)  // 5MB

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf-1", "run-1")
    }
}
```

Run:
```bash
cd codec-server
go test -bench=. -benchmem ./internal/codec
```

## Production Validation

### Pre-Deployment Checklist

- [ ] All unit tests pass
- [ ] Integration tests pass
- [ ] Performance tests meet targets
- [ ] Security scan completed
- [ ] IAM roles configured
- [ ] S3 bucket created with encryption
- [ ] Monitoring configured
- [ ] Runbooks prepared

### Canary Deployment

1. **Deploy to canary environment**
```bash
kubectl apply -f k8s/codec-server-deployment.yaml -n temporal-canary
```

2. **Route 10% traffic to canary**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: codec-server
spec:
  selector:
    app: codec-server
    version: canary
  # ... rest of config
```

3. **Monitor metrics**
- Error rates
- Latency
- S3 costs
- Resource usage

4. **Gradually increase traffic**
- 10% → 25% → 50% → 100%

### Smoke Tests

After deployment:

```bash
# Test health endpoint
kubectl run test-pod --rm -it --image=curlimages/curl -- \
  curl http://codec-server.temporal:8080/health

# Test encode endpoint
kubectl run test-pod --rm -it --image=curlimages/curl -- \
  curl -X POST http://codec-server.temporal:8080/encode \
  -H "Content-Type: application/json" \
  -d '{"payloads":[],"namespace":"default","workflowId":"test","runId":"test"}'

# Check metrics
kubectl run test-pod --rm -it --image=curlimages/curl -- \
  curl http://codec-server.temporal:8080/metrics
```

### Monitoring Checklist

- [ ] Prometheus scraping metrics
- [ ] Grafana dashboards configured
- [ ] Alerts configured (error rate, latency)
- [ ] Log aggregation working
- [ ] S3 costs monitored

## Troubleshooting

### Common Issues

#### Issue: S3 Upload Fails

**Symptoms:**
```
Failed to upload to S3: AccessDenied
```

**Resolution:**
1. Check IAM policy
```bash
aws iam get-role-policy --role-name codec-server-role --policy-name CodecServerS3Access
```

2. Verify service account annotation
```bash
kubectl get sa codec-server-sa -n temporal -o yaml
```

3. Test S3 access from pod
```bash
kubectl exec -it codec-server-xxx -n temporal -- \
  aws s3 ls s3://temporal-large-payloads-prod/
```

#### Issue: High Latency

**Symptoms:**
- Encode latency > 1s
- Workflows slow

**Resolution:**
1. Check S3 region
```yaml
S3_REGION: "us-east-1"  # Must match your region
```

2. Monitor S3 metrics
```bash
aws cloudwatch get-metric-statistics \
  --namespace AWS/S3 \
  --metric-name AllRequests \
  --dimensions Name=BucketName,Value=temporal-large-payloads-prod \
  --statistics Sum \
  --start-time 2024-01-01T00:00:00Z \
  --end-time 2024-01-01T23:59:59Z \
  --period 3600
```

3. Scale codec server
```bash
kubectl scale deployment codec-server -n temporal --replicas=10
```

#### Issue: Memory Leak

**Symptoms:**
- Memory usage grows over time
- OOMKilled pods

**Resolution:**
1. Check metrics
```bash
curl http://localhost:8080/metrics | grep bytes
```

2. Adjust threshold
```yaml
PAYLOAD_SIZE_THRESHOLD_BYTES: "1048576"  # 1MB instead of 2MB
```

3. Increase memory limits
```yaml
resources:
  limits:
    memory: "1Gi"
```

### Debug Mode

Enable debug logging:

```bash
# Set in deployment
env:
  - name: LOG_LEVEL
    value: "debug"

# Or restart with debug
kubectl set env deployment/codec-server LOG_LEVEL=debug -n temporal
```

View debug logs:
```bash
kubectl logs -f deployment/codec-server -n temporal | grep DEBUG
```

## Test Data

### Generate Test Files

```bash
# Generate 1MB file
dd if=/dev/urandom of=test-1mb.bin bs=1M count=1

# Generate 5MB file
dd if=/dev/urandom of=test-5mb.bin bs=1M count=5

# Generate 10MB file
dd if=/dev/urandom of=test-10mb.bin bs=1M count=10
```

### Clean Up Test Data

```bash
# Clean S3
aws --endpoint-url=http://localhost:4566 s3 rm s3://temporal-large-payloads/ --recursive

# Clean Temporal workflows
# Via UI: http://localhost:8233

# Restart services
docker-compose down
docker-compose up -d
```

## Continuous Testing

### GitHub Actions

Create `.github/workflows/test.yml`:

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Set up Go
        uses: actions/setup-go@v2
        with:
          go-version: 1.21

      - name: Run tests
        run: |
          cd codec-server
          go test -v ./...

      - name: Integration tests
        run: |
          docker-compose up -d
          sleep 30
          ./scripts/test-large-payload.sh
```

## Metrics to Track

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `codec_encode_errors_total` | Encode failures | > 1% |
| `codec_s3_upload_errors_total` | S3 upload failures | > 0.5% |
| `codec_avg_encode_latency_ms` | Avg encode time | > 500ms |
| `codec_largest_payload_bytes` | Largest payload | > 100MB |
| `codec_bytes_uploaded_total` | S3 usage trend | Track costs |

## Next Steps

After successful testing:

1. Document any issues found
2. Update runbooks
3. Train operations team
4. Set up on-call rotation
5. Schedule regular load tests
6. Review and optimize costs
