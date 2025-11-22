# Remaining Work Plan - Production Readiness

This document outlines the remaining work needed to achieve full production readiness for the Temporal Large Files Codec Server project.

**Last Updated**: 2025-11-22
**Current Status**: Core functionality complete, optional improvements and production tooling needed

---

## Priority Classification

- **P0 - CRITICAL**: Blocks production deployment, must fix immediately
- **P1 - HIGH**: Required for production readiness, should complete before launch
- **P2 - MEDIUM**: Important for operational excellence, complete within first month
- **P3 - LOW**: Nice to have, can be addressed post-launch

---

## 🚨 P0 - CRITICAL Issues (BLOCKING)

### 1. Fix Missing Error Type Definition
**Status**: ❌ Not Started
**Effort**: 15 minutes
**Impact**: Tests won't compile

**Problem**:
- Tests reference `storage.ErrNotFound` which is not defined
- Affects 3 test files: `integration_test.go`, `grpc/server_test.go`, `http/server_test.go`
- Will cause compilation errors when running `go test`

**Solution**:
```go
// Create: codec-server/internal/storage/errors.go
package storage

import "errors"

var (
    ErrNotFound      = errors.New("object not found in storage")
    ErrAlreadyExists = errors.New("object already exists")
    ErrInvalidKey    = errors.New("invalid storage key")
)
```

**Acceptance Criteria**:
- [ ] Create `codec-server/internal/storage/errors.go`
- [ ] Define `ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidKey`
- [ ] Run `go test ./...` to verify compilation
- [ ] All tests pass

---

### 2. Fix Go Module Dependencies
**Status**: ❌ Not Started
**Effort**: 5 minutes
**Impact**: Tests and builds won't work

**Problem**:
- Missing go.sum entries for AWS SDK packages
- Will cause `go test` and `go build` failures

**Solution**:
```bash
cd codec-server
go mod tidy
go mod download
```

**Acceptance Criteria**:
- [ ] Run `go mod tidy` in codec-server directory
- [ ] Verify `go.sum` updated
- [ ] Confirm `go test ./...` works
- [ ] Confirm `go build ./cmd/codec-server` works

---

## 📋 P1 - HIGH Priority (Required for Production)

### 3. CI/CD Pipeline Implementation
**Status**: ❌ Not Started
**Effort**: 4-6 hours
**Impact**: No automated testing, manual deployment only

**Problem**:
- No automated testing on pull requests
- No automated builds or deployments
- No security scanning in build pipeline

**Solution**:
Create `.github/workflows/ci.yml`:

```yaml
name: CI/CD Pipeline

on:
  push:
    branches: [ main, develop, claude/* ]
  pull_request:
    branches: [ main, develop ]

jobs:
  test-codec-server:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - name: Run tests
        working-directory: ./codec-server
        run: |
          go mod download
          go test -v -race -coverprofile=coverage.out ./...
          go tool cover -html=coverage.out -o coverage.html

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./codec-server/coverage.out

  test-cleanup-worker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - name: Run tests
        working-directory: ./cleanup-worker
        run: |
          go mod download
          go test -v -race ./...

  test-typescript-worker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install dependencies
        working-directory: ./samples/typescript-worker
        run: npm ci

      - name: Run tests
        working-directory: ./samples/typescript-worker
        run: npm test

  security-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run Trivy vulnerability scanner
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'fs'
          scan-ref: '.'
          format: 'sarif'
          output: 'trivy-results.sarif'

      - name: Upload Trivy results to GitHub Security
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: 'trivy-results.sarif'

  build-and-push:
    runs-on: ubuntu-latest
    needs: [test-codec-server, test-cleanup-worker, security-scan]
    if: github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push codec-server
        uses: docker/build-push-action@v5
        with:
          context: ./codec-server
          push: true
          tags: |
            ghcr.io/${{ github.repository }}/codec-server:latest
            ghcr.io/${{ github.repository }}/codec-server:${{ github.sha }}

      - name: Build and push cleanup-worker
        uses: docker/build-push-action@v5
        with:
          context: ./cleanup-worker
          push: true
          tags: |
            ghcr.io/${{ github.repository }}/cleanup-worker:latest
            ghcr.io/${{ github.repository }}/cleanup-worker:${{ github.sha }}
```

**Acceptance Criteria**:
- [ ] Create `.github/workflows/ci.yml`
- [ ] All tests run on every PR
- [ ] Security scanning runs on every push
- [ ] Docker images build and push on main branch
- [ ] Coverage reports uploaded to Codecov

---

### 4. Prometheus Alerting Rules
**Status**: ❌ Not Started
**Effort**: 2-3 hours
**Impact**: No proactive monitoring alerts

**Problem**:
- Metrics are exposed but no alerts configured
- Issues won't be detected until manual inspection

**Solution**:
Create `monitoring/prometheus-rules.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-codec-server-rules
  namespace: temporal
data:
  codec-server.rules: |
    groups:
      - name: codec_server_alerts
        interval: 30s
        rules:
          # High error rate alert
          - alert: CodecHighErrorRate
            expr: |
              (
                rate(codec_encode_errors_total[5m]) +
                rate(codec_decode_errors_total[5m])
              ) > 0.05
            for: 5m
            labels:
              severity: warning
              component: codec-server
            annotations:
              summary: "High error rate in codec server"
              description: "Error rate is {{ $value | humanizePercentage }} over the last 5 minutes"

          # Service unhealthy alert
          - alert: CodecServiceUnhealthy
            expr: up{job="codec-server"} == 0
            for: 2m
            labels:
              severity: critical
              component: codec-server
            annotations:
              summary: "Codec server is down"
              description: "Codec server has been unavailable for 2 minutes"

          # High latency alert
          - alert: CodecHighLatency
            expr: |
              histogram_quantile(0.95,
                rate(codec_encode_duration_seconds_bucket[5m])
              ) > 2.0
            for: 5m
            labels:
              severity: warning
              component: codec-server
            annotations:
              summary: "High encode latency detected"
              description: "P95 encode latency is {{ $value }}s"

          # S3 upload failures
          - alert: CodecS3UploadFailures
            expr: rate(s3_upload_errors_total[5m]) > 0.01
            for: 5m
            labels:
              severity: warning
              component: codec-server
            annotations:
              summary: "S3 upload failures detected"
              description: "S3 upload failure rate: {{ $value | humanizePercentage }}"

          # High memory usage
          - alert: CodecHighMemoryUsage
            expr: |
              container_memory_usage_bytes{pod=~"codec-server-.*"} /
              container_spec_memory_limit_bytes{pod=~"codec-server-.*"} > 0.85
            for: 10m
            labels:
              severity: warning
              component: codec-server
            annotations:
              summary: "High memory usage in codec server"
              description: "Memory usage is at {{ $value | humanizePercentage }}"

          # Payload size anomaly
          - alert: CodecUnusualPayloadSize
            expr: |
              max(codec_largest_payload_bytes) > 100000000
            for: 5m
            labels:
              severity: info
              component: codec-server
            annotations:
              summary: "Unusually large payload detected"
              description: "Largest payload: {{ $value | humanize }}B"
```

**Acceptance Criteria**:
- [ ] Create `monitoring/prometheus-rules.yaml`
- [ ] Deploy to Kubernetes cluster
- [ ] Verify alerts show in Prometheus UI
- [ ] Configure AlertManager routing
- [ ] Test alert firing with simulated errors

---

### 5. Grafana Dashboards
**Status**: ❌ Not Started
**Effort**: 3-4 hours
**Impact**: No visualization of metrics

**Solution**:
Create `monitoring/grafana-dashboard-codec-server.json` with panels for:
- Request rate (encode/decode)
- Error rate and count
- Latency percentiles (p50, p95, p99)
- S3 operations (upload/download rate, errors)
- Payload size distribution
- Memory and CPU usage
- Active connections

**Acceptance Criteria**:
- [ ] Create Grafana dashboard JSON
- [ ] Import to Grafana instance
- [ ] Verify all panels show data
- [ ] Add to Grafana provisioning in K8s

---

## 🔧 P2 - MEDIUM Priority (Operational Excellence)

### 6. Fix TypeScript Remote Codec Metadata
**Status**: ❌ Not Started
**Effort**: 2-3 hours
**Impact**: TypeScript workers send "unknown" for workflow context

**Problem**:
Lines 36-37 in `samples/typescript-worker/src/codec/remote-codec.ts`:
```typescript
workflowId: 'unknown',
runId: 'unknown',
```

**Solution**:
Similar to Go implementation, extract workflow context from Temporal's activity/workflow context:

```typescript
import { workflowInfo } from '@temporalio/workflow';
import { Context as ActivityContext } from '@temporalio/activity';

export class RemoteCodec implements PayloadCodec {
  async encode(payloads: Payload[]): Promise<Payload[]> {
    // Try to extract workflow context
    let workflowId = 'unknown';
    let runId = 'unknown';
    let namespace = 'default';

    try {
      // In workflow context
      const info = workflowInfo();
      workflowId = info.workflowId;
      runId = info.runId;
      namespace = info.namespace;
    } catch {
      // Not in workflow context, try activity context
      try {
        const activityInfo = ActivityContext.current().info;
        workflowId = activityInfo.workflowExecution.workflowId;
        runId = activityInfo.workflowExecution.runId;
        namespace = activityInfo.workflowNamespace;
      } catch {
        // Fall back to extracting from payload metadata
        for (const payload of payloads) {
          if (payload.metadata?.['temporal-workflow-id']) {
            workflowId = Buffer.from(payload.metadata['temporal-workflow-id']).toString();
          }
          if (payload.metadata?.['temporal-run-id']) {
            runId = Buffer.from(payload.metadata['temporal-run-id']).toString();
          }
        }
      }
    }

    const response = await fetch(`${this.codecServerUrl}/encode`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        payloads: serializedPayloads,
        namespace,
        workflowId,
        runId,
      }),
    });
    // ...
  }
}
```

**Acceptance Criteria**:
- [ ] Update `remote-codec.ts` with context extraction
- [ ] Test with actual TypeScript workflow
- [ ] Verify S3 keys contain real workflow IDs
- [ ] Document in README

---

### 7. Load Testing Implementation
**Status**: ❌ Not Started
**Effort**: 4-6 hours
**Impact**: Unknown performance under load

**Solution**:
Create `tests/load-test.js` using k6:

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '2m', target: 10 },   // Ramp up to 10 users
    { duration: '5m', target: 10 },   // Stay at 10 users
    { duration: '2m', target: 50 },   // Ramp up to 50 users
    { duration: '5m', target: 50 },   // Stay at 50 users
    { duration: '2m', target: 100 },  // Ramp up to 100 users
    { duration: '5m', target: 100 },  // Stay at 100 users
    { duration: '2m', target: 0 },    // Ramp down to 0 users
  ],
  thresholds: {
    'http_req_duration': ['p(95)<2000'], // 95% of requests must complete below 2s
    'errors': ['rate<0.05'],             // Error rate must be below 5%
  },
};

const CODEC_SERVER_URL = __ENV.CODEC_SERVER_URL || 'http://localhost:8080';

export default function() {
  // Test small payload encode
  const smallPayload = {
    payloads: [{
      metadata: { encoding: 'json/plain' },
      data: btoa('small test data'),
    }],
    namespace: 'load-test',
    workflowId: `workflow-${__VU}-${__ITER}`,
    runId: `run-${__VU}-${__ITER}`,
  };

  let res = http.post(`${CODEC_SERVER_URL}/encode`, JSON.stringify(smallPayload), {
    headers: { 'Content-Type': 'application/json' },
  });

  const success = check(res, {
    'encode status 200': (r) => r.status === 200,
    'encode has payloads': (r) => JSON.parse(r.body).payloads.length > 0,
  });

  errorRate.add(!success);

  // Test large payload encode (3MB)
  const largeData = 'x'.repeat(3 * 1024 * 1024);
  const largePayload = {
    payloads: [{
      metadata: { encoding: 'binary/octet-stream' },
      data: btoa(largeData),
    }],
    namespace: 'load-test',
    workflowId: `workflow-large-${__VU}-${__ITER}`,
    runId: `run-large-${__VU}-${__ITER}`,
  };

  res = http.post(`${CODEC_SERVER_URL}/encode`, JSON.stringify(largePayload), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(res, {
    'large encode status 200': (r) => r.status === 200,
  });

  sleep(1);
}
```

**Acceptance Criteria**:
- [ ] Create `tests/load-test.js`
- [ ] Run baseline load test
- [ ] Document performance baselines
- [ ] Add to CI/CD pipeline (nightly)
- [ ] Create performance regression alerts

---

### 8. Helm Chart Creation
**Status**: ❌ Not Started
**Effort**: 6-8 hours
**Impact**: Difficult to deploy across environments

**Solution**:
Create Helm charts:
```
helm/
├── codec-server/
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── values-dev.yaml
│   ├── values-staging.yaml
│   ├── values-prod.yaml
│   └── templates/
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── hpa.yaml
│       ├── pdb.yaml
│       ├── configmap.yaml
│       ├── secrets.yaml
│       └── servicemonitor.yaml
└── cleanup-worker/
    ├── Chart.yaml
    ├── values.yaml
    └── templates/
        ├── deployment.yaml
        └── configmap.yaml
```

**Acceptance Criteria**:
- [ ] Create Helm charts for codec-server and cleanup-worker
- [ ] Support environment-specific values files
- [ ] Test deployment to dev/staging/prod
- [ ] Document Helm installation in README
- [ ] Publish to Helm repository

---

### 9. Disaster Recovery Documentation
**Status**: ❌ Not Started
**Effort**: 3-4 hours
**Impact**: No recovery procedures if failure occurs

**Solution**:
Create `docs/DISASTER_RECOVERY.md`:

```markdown
# Disaster Recovery Procedures

## RTO/RPO Specifications
- **RTO (Recovery Time Objective)**: 1 hour
- **RPO (Recovery Point Objective)**: 5 minutes

## Backup Strategy

### S3 Bucket Backup
- Enable S3 versioning on payload bucket
- Configure lifecycle policy for 90-day retention
- Enable cross-region replication to us-west-2

### Temporal Workflow State
- Temporal automatically backs up workflow history
- PostgreSQL database backups every 6 hours
- Point-in-time recovery available for 7 days

## Recovery Procedures

### Scenario 1: Codec Server Outage
1. Check pod status: `kubectl get pods -n temporal`
2. Check logs: `kubectl logs -n temporal <pod-name>`
3. Check health endpoint: `curl http://codec-server/health`
4. If unhealthy, restart deployment:
   ```bash
   kubectl rollout restart deployment/codec-server -n temporal
   ```
5. Monitor recovery: `kubectl rollout status deployment/codec-server -n temporal`

### Scenario 2: S3 Bucket Unavailable
1. Check S3 service health
2. Verify IAM permissions
3. Test S3 connectivity from pod:
   ```bash
   kubectl exec -it <pod-name> -- aws s3 ls s3://temporal-payloads/
   ```
4. If cross-region replication configured, failover to replica bucket

### Scenario 3: Data Corruption
1. Identify affected workflow IDs from monitoring
2. List corrupted objects:
   ```bash
   aws s3 ls s3://temporal-payloads/<namespace>/<workflow-id>/
   ```
3. Restore from S3 versioning:
   ```bash
   aws s3api list-object-versions --bucket temporal-payloads --prefix <key>
   aws s3api get-object --bucket temporal-payloads --key <key> --version-id <id> <output-file>
   ```
4. Re-upload restored data

## Testing Recovery
- Monthly DR drill scheduled
- Simulate pod failure and verify auto-recovery
- Test S3 restore from version history
- Document recovery time for each scenario
```

**Acceptance Criteria**:
- [ ] Document RTO/RPO targets
- [ ] Define backup procedures
- [ ] Write recovery runbooks
- [ ] Schedule monthly DR drills
- [ ] Validate recovery procedures

---

## 📝 P3 - LOW Priority (Post-Launch Improvements)

### 10. Enhanced Security Scanning
**Status**: ❌ Not Started
**Effort**: 4-6 hours

**Tasks**:
- [ ] Add CodeQL scanning to GitHub Actions
- [ ] Configure Dependabot for dependency updates
- [ ] Add SAST scanning (Semgrep or SonarQube)
- [ ] Implement container image scanning in CI/CD
- [ ] Set up security advisory notifications

---

### 11. Advanced Monitoring
**Status**: ❌ Not Started
**Effort**: 6-8 hours

**Tasks**:
- [ ] Add distributed tracing (OpenTelemetry)
- [ ] Implement request correlation IDs
- [ ] Add structured logging with context
- [ ] Create runbook links in alerts
- [ ] Set up on-call rotation in PagerDuty

---

### 12. Performance Optimizations
**Status**: ❌ Not Started
**Effort**: 8-12 hours

**Tasks**:
- [ ] Implement S3 multipart upload for large files
- [ ] Add connection pooling tuning
- [ ] Implement payload compression before S3 upload
- [ ] Add caching layer for frequently accessed payloads
- [ ] Optimize protobuf serialization

---

### 13. Documentation Improvements
**Status**: ❌ Not Started
**Effort**: 4-6 hours

**Tasks**:
- [ ] Create architecture decision records (ADRs)
- [ ] Add API documentation (OpenAPI/Swagger)
- [ ] Create video tutorials
- [ ] Write troubleshooting playbooks
- [ ] Add FAQ section

---

## Summary

### Critical Path to Production (P0 + P1)

**Estimated Total Effort**: 12-18 hours

1. Fix missing error types (15 min)
2. Fix Go module dependencies (5 min)
3. CI/CD pipeline (4-6 hours)
4. Prometheus alerting rules (2-3 hours)
5. Grafana dashboards (3-4 hours)

### Quick Wins (Can complete in 1 day)

- ✅ P0 items (20 minutes total)
- ✅ CI/CD pipeline (4-6 hours)
- ✅ Alerting rules (2-3 hours)

**Total**: ~7-9 hours for core production requirements

### Recommended Sequence

**Week 1** (Must complete before production):
1. Day 1: Fix P0 issues (error types, go modules)
2. Day 2-3: Implement CI/CD pipeline
3. Day 4: Set up Prometheus alerts and Grafana dashboards

**Week 2-3** (Complete for operational excellence):
4. Fix TypeScript codec metadata
5. Implement load testing
6. Create Helm charts
7. Write disaster recovery docs

**Month 2+** (Ongoing improvements):
8. Enhanced security scanning
9. Advanced monitoring
10. Performance optimizations
11. Documentation improvements

---

## Testing the Plan

Before marking items complete, verify:

- [ ] All tests pass: `go test ./...`
- [ ] Docker builds work: `docker build .`
- [ ] Kubernetes deploys successfully
- [ ] Monitoring shows green across all dashboards
- [ ] Load tests meet performance SLAs
- [ ] DR procedures tested and documented

---

## Approval & Sign-off

This plan should be reviewed and approved by:
- [ ] Development team
- [ ] DevOps/SRE team
- [ ] Security team
- [ ] Product/Project owner

**Next Steps**: Start with P0 critical issues immediately.
