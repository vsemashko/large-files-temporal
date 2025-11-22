# Load Testing

This directory contains load testing scripts for the Temporal Large Files Codec Server.

## Prerequisites

- [k6](https://k6.io/) installed
- Codec server running and accessible

### Install k6

**macOS:**
```bash
brew install k6
```

**Linux (Debian/Ubuntu):**
```bash
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

**Using mise (recommended):**
```bash
# Already configured in .tool-versions
mise install k6
```

## Running Load Tests

### Quick Test (Default Configuration)

```bash
k6 run load-test.js
```

### Against Local Server

```bash
CODEC_SERVER_URL=http://localhost:8080 k6 run load-test.js
```

### Against Remote Server

```bash
CODEC_SERVER_URL=https://codec-server.example.com k6 run load-test.js
```

### With API Key Authentication

```bash
CODEC_SERVER_URL=http://localhost:8080 \
API_KEY=your-secret-api-key \
k6 run load-test.js
```

### Custom Load Profile

Override stages with command-line options:

```bash
# Short smoke test (2 minutes)
k6 run --duration 2m --vus 10 load-test.js

# Stress test (high load)
k6 run --duration 10m --vus 500 load-test.js

# Spike test
k6 run --stage 0s:0,10s:1000,1m:1000,10s:0 load-test.js
```

## Test Scenarios

The load test includes multiple scenarios with different payload sizes:

| Scenario | % of Traffic | Payload Size | Purpose |
|----------|--------------|--------------|---------|
| Health Check | 10% | N/A | Monitor service health |
| Small Payload Roundtrip | 40% | 1KB | Test inline encoding |
| Medium Payload | 20% | 512KB | Test near-threshold handling |
| Large Payload | 10% | 3MB | Test S3 storage path |
| Multiple Payloads | 20% | 5x1KB | Test batch processing |

## Load Profile

Default stages (total ~25 minutes):

1. **Warmup** (1m): 0 → 5 VUs
2. **Ramp-up** (11m): 5 → 10 → 20 → 30 → 50 VUs
3. **Peak Load** (5m): 100 VUs
4. **Stress Test** (2m): 150 VUs (optional)
5. **Cool Down** (5m): 150 → 50 → 10 → 0 VUs

Estimated peak RPS: ~200-300 requests/second

## Performance Thresholds

The test enforces these SLAs:

| Metric | Threshold | Description |
|--------|-----------|-------------|
| P95 Latency | < 2s | 95% of requests complete within 2s |
| P99 Latency | < 5s | 99% of requests complete within 5s |
| Error Rate | < 5% | Less than 5% of requests fail |
| Encode P95 | < 2s | Encode operations complete quickly |
| Decode P95 | < 2s | Decode operations complete quickly |
| Large Payload P95 | < 5s | S3 operations complete within 5s |
| Large Payload P99 | < 10s | Even slow S3 ops complete within 10s |

## Interpreting Results

### Successful Test

```
✓ encode status 200
✓ encode has payloads
✓ decode status 200

http_req_duration..............: avg=850ms  min=120ms med=650ms max=4.2s  p(95)=1.8s  p(99)=3.5s
errors.........................: 0.02% ✓ 125  ✗ 625000
```

### Failed Test

```
✗ encode status 200          95.2% — ✓ 119000  ✗ 6000
✗ http_req_duration...<2000  91.5% — threshold violation

http_req_duration..............: avg=2.5s   min=120ms med=2.1s max=28s   p(95)=5.2s  p(99)=15s
errors.........................: 4.8%  ✓ 6000   ✗ 119000
```

## Custom Metrics

In addition to standard k6 metrics, we track:

- `encode_latency` - Time to encode payloads
- `decode_latency` - Time to decode payloads
- `large_payload_latency` - Time to process large (>2MB) payloads
- `payloads_sent` - Total number of payloads sent
- `payloads_received` - Total number of payloads received
- `errors` - Custom error rate counter

## Analyzing Results

### View Results in Real-Time

```bash
k6 run load-test.js --out influxdb=http://localhost:8086/k6
```

### Save Results to JSON

```bash
k6 run load-test.js --out json=load-test-results.json
```

Results are automatically saved to `load-test-results.json` after each run.

### Generate HTML Report

```bash
# Requires k6-reporter
npm install -g k6-to-junit
k6 run load-test.js --out junit=results.xml
```

### Send to Prometheus

```bash
# Requires Prometheus remote write
k6 run load-test.js --out experimental-prometheus-rw
```

### Send to Grafana Cloud

```bash
K6_CLOUD_TOKEN=your-token k6 cloud load-test.js
```

## Baseline Performance

Expected performance on recommended infrastructure (3 replicas, 2 CPU, 4GB RAM each):

| Metric | Target | Acceptable | Poor |
|--------|--------|------------|------|
| P95 Latency | < 500ms | < 2s | > 2s |
| P99 Latency | < 1s | < 5s | > 5s |
| Error Rate | < 0.1% | < 5% | > 5% |
| Throughput | > 500 RPS | > 200 RPS | < 200 RPS |
| S3 Upload P95 | < 2s | < 5s | > 5s |

## Troubleshooting

### High Error Rate

**Symptom:** Error rate > 5%

**Possible Causes:**
- S3 connectivity issues
- Insufficient resources (CPU/memory)
- Rate limiting kicking in
- Database connection exhaustion

**Investigation:**
```bash
# Check server logs
kubectl logs -n temporal deployment/codec-server --tail=100

# Check Prometheus metrics
curl http://codec-server:8080/metrics | grep error

# Check pod resources
kubectl top pods -n temporal
```

### High Latency

**Symptom:** P95 > 2s or P99 > 5s

**Possible Causes:**
- S3 latency
- CPU throttling
- Memory pressure
- Network congestion

**Investigation:**
```bash
# Check S3 latency specifically
k6 run --env LARGE_ONLY=true load-test.js

# Profile the server
go tool pprof http://codec-server:8080/debug/pprof/profile?seconds=30
```

### Connection Errors

**Symptom:** Connection refused or timeout errors

**Possible Causes:**
- Server not running
- Wrong URL
- Firewall blocking
- Service not exposed

**Investigation:**
```bash
# Verify server is running
kubectl get pods -n temporal | grep codec-server

# Test connectivity
curl http://codec-server:8080/health

# Check service
kubectl get svc -n temporal codec-server
```

## Continuous Performance Testing

### Run in CI/CD

Example GitHub Actions:

```yaml
- name: Run load test
  run: |
    k6 run --duration 5m --vus 50 tests/load-test.js
  env:
    CODEC_SERVER_URL: ${{ secrets.STAGING_URL }}
```

### Scheduled Tests

Run daily performance regression tests:

```yaml
# .github/workflows/performance.yml
on:
  schedule:
    - cron: '0 2 * * *'  # 2 AM daily

jobs:
  load-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install k6
        run: |
          sudo gpg -k
          sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
          echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
          sudo apt-get update
          sudo apt-get install k6

      - name: Run load test
        run: k6 run tests/load-test.js
        env:
          CODEC_SERVER_URL: https://codec-server-staging.example.com
```

## Best Practices

1. **Start Small**: Begin with low VUs and gradually increase
2. **Monitor Server**: Watch server metrics during tests
3. **Baseline First**: Establish baseline performance before changes
4. **Consistent Environment**: Use same infrastructure for comparisons
5. **Cleanup**: Clean up S3 test data after large payload tests
6. **Document Results**: Track performance over time

## Related Documentation

- [TESTING.md](../TESTING.md) - Comprehensive testing guide
- [Prometheus Monitoring](../monitoring/README.md) - Metrics and alerts
- [Production Readiness](../PRODUCTION_READINESS_COMPLETE.md) - Production deployment guide
