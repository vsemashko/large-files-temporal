# Monitoring Configuration

This directory contains monitoring and alerting configuration for the Temporal Large Files Codec Server.

## Contents

- `prometheus-rules.yaml` - Prometheus alerting rules for codec server and cleanup worker
- `grafana-dashboard-codec-server.json` - Grafana dashboard for visualization
- `alertmanager-config.yaml` - Example AlertManager configuration (optional)

## Deploying Prometheus Rules

### Prerequisites

- Kubernetes cluster with Prometheus Operator installed
- `kubectl` configured to access your cluster
- Codec server deployed with Prometheus metrics enabled

### Deployment

```bash
# Deploy the Prometheus rules ConfigMap
kubectl apply -f prometheus-rules.yaml

# If using Prometheus Operator, create a PrometheusRule resource
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: codec-server-alerts
  namespace: temporal
  labels:
    prometheus: kube-prometheus
    role: alert-rules
spec:
  groups:
$(cat prometheus-rules.yaml | grep -A 1000 "codec-server.rules:" | tail -n +2 | sed 's/^/    /')
EOF
```

### Verification

```bash
# Check if rules are loaded in Prometheus
kubectl port-forward -n monitoring svc/prometheus-operated 9090:9090

# Then visit http://localhost:9090/rules
# You should see all codec-server alerts listed
```

## Alert Severity Levels

- **critical** - Immediate action required, service degraded
- **warning** - Action required within business hours
- **info** - Informational, may indicate optimization opportunities

## Alert Groups

### 1. Availability (`codec_server_availability`)
- `CodecServerDown` - Service completely unavailable
- `CodecServerUnhealthy` - Health checks failing
- `CodecServerQuorumLoss` - Multiple instances down

### 2. Error Rates (`codec_server_errors`)
- `CodecHighErrorRate` - Error rate > 5%
- `CodecCriticalErrorRate` - Error rate > 20%
- `CodecS3UploadFailures` - S3 upload issues
- `CodecS3DownloadFailures` - S3 download issues

### 3. Latency (`codec_server_latency`)
- `CodecHighEncodeLatency` - P95 encode > 2s
- `CodecCriticalEncodeLatency` - P95 encode > 5s
- `CodecHighDecodeLatency` - P95 decode > 2s
- `CodecSlowS3Operations` - P95 S3 ops > 3s

### 4. Resources (`codec_server_resources`)
- `CodecHighMemoryUsage` - Memory > 85%
- `CodecCriticalMemoryUsage` - Memory > 95%
- `CodecHighCPUUsage` - CPU > 80%
- `CodecFrequentRestarts` - Pod restarting frequently

### 5. Traffic (`codec_server_traffic`)
- `CodecTrafficSpike` - 3x normal traffic
- `CodecNoTraffic` - No requests for 30m

### 6. Data Anomalies (`codec_server_data_anomalies`)
- `CodecUnusualPayloadSize` - Payload > 100MB
- `CodecHighS3StorageRate` - High upload rate

### 7. Cleanup Worker (`cleanup_worker_health`)
- `CleanupWorkerDown` - Cleanup worker offline
- `CleanupWorkerErrors` - High cleanup error rate

## AlertManager Integration

Example AlertManager configuration for routing alerts:

```yaml
# alertmanager-config.yaml
route:
  group_by: ['alertname', 'component']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'team-platform'
  routes:
    - match:
        severity: critical
        component: codec-server
      receiver: 'pagerduty-critical'
      continue: true

    - match:
        severity: warning
        component: codec-server
      receiver: 'slack-warnings'

receivers:
  - name: 'team-platform'
    slack_configs:
      - api_url: 'YOUR_SLACK_WEBHOOK_URL'
        channel: '#platform-alerts'
        title: '{{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'

  - name: 'pagerduty-critical'
    pagerduty_configs:
      - service_key: 'YOUR_PAGERDUTY_KEY'
        description: '{{ .GroupLabels.alertname }}'

  - name: 'slack-warnings'
    slack_configs:
      - api_url: 'YOUR_SLACK_WEBHOOK_URL'
        channel: '#platform-alerts'
```

## Testing Alerts

### Simulate High Error Rate

```bash
# Use k6 to send invalid requests
k6 run --vus 10 --duration 30s - <<EOF
import http from 'k6/http';

export default function() {
  http.post('http://codec-server:8080/encode',
    'invalid json',
    { headers: { 'Content-Type': 'application/json' } }
  );
}
EOF
```

### Simulate High Latency

```bash
# Send large payloads to increase latency
for i in {1..100}; do
  dd if=/dev/urandom bs=1M count=10 | base64 | \
    curl -X POST http://codec-server:8080/encode \
      -H "Content-Type: application/json" \
      -d @- &
done
```

### Simulate Service Down

```bash
# Scale down codec-server to 0 replicas
kubectl scale deployment codec-server -n temporal --replicas=0

# Wait 2+ minutes
# Check Prometheus alerts

# Scale back up
kubectl scale deployment codec-server -n temporal --replicas=3
```

## Runbooks

Create runbooks at `docs/runbooks/` for each alert:

- `codec-server-down.md` - Steps to recover from service outage
- `health-check-failure.md` - Debug health check issues
- `high-error-rate.md` - Investigate and fix error spikes
- `high-latency.md` - Performance troubleshooting

## Metrics Reference

All metrics exposed by codec-server:

| Metric | Type | Description |
|--------|------|-------------|
| `codec_encode_requests_total` | Counter | Total encode requests |
| `codec_decode_requests_total` | Counter | Total decode requests |
| `codec_encode_errors_total` | Counter | Total encode errors |
| `codec_decode_errors_total` | Counter | Total decode errors |
| `codec_payloads_encoded_total` | Counter | Total payloads encoded |
| `codec_payloads_decoded_total` | Counter | Total payloads decoded |
| `s3_uploads_total` | Counter | Total S3 uploads |
| `s3_downloads_total` | Counter | Total S3 downloads |
| `s3_upload_errors_total` | Counter | S3 upload errors |
| `s3_download_errors_total` | Counter | S3 download errors |
| `s3_bytes_uploaded_total` | Counter | Total bytes uploaded to S3 |
| `s3_bytes_downloaded_total` | Counter | Total bytes downloaded from S3 |
| `codec_largest_payload_bytes` | Gauge | Largest payload size |
| `codec_smallest_payload_bytes` | Gauge | Smallest payload size |
| `codec_encode_duration_seconds` | Histogram | Encode operation duration |
| `codec_decode_duration_seconds` | Histogram | Decode operation duration |
| `s3_operation_duration_seconds` | Histogram | S3 operation duration |

## Grafana Dashboard

Import the Grafana dashboard:

```bash
# Using Grafana API
curl -X POST http://grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d @grafana-dashboard-codec-server.json

# Or manually import via Grafana UI:
# 1. Go to Dashboards > Import
# 2. Upload grafana-dashboard-codec-server.json
# 3. Select Prometheus data source
```

## Support

For questions or issues:
- GitHub Issues: https://github.com/vsemashko/large-files-temporal/issues
- Documentation: https://github.com/vsemashko/large-files-temporal/docs
