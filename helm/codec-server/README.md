# Codec Server Helm Chart

Helm chart for deploying Temporal Large Files Codec Server to Kubernetes.

## Installation

### Quick Start (Development)

```bash
helm install codec-server ./helm/codec-server \
  --namespace temporal \
  --create-namespace
```

### Production Deployment

```bash
# Install with production values
helm install codec-server ./helm/codec-server \
  --namespace temporal \
  --create-namespace \
  --values ./helm/codec-server/values-prod.yaml
```

## Configuration

See [values.yaml](values.yaml) for all configuration options.

### Key Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `3` |
| `config.s3.bucket` | S3 bucket name | `temporal-large-payloads` |
| `config.s3.region` | AWS region | `us-east-1` |
| `config.largePayloadThreshold` | Payload size threshold (bytes) | `2097152` (2MB) |
| `config.tls.enabled` | Enable TLS | `false` |
| `config.auth.enabled` | Enable API key auth | `false` |
| `resources.requests.cpu` | CPU request | `500m` |
| `resources.requests.memory` | Memory request | `1Gi` |
| `autoscaling.enabled` | Enable HPA | `true` |

### Environment-Specific Values

Create environment-specific values files:

**values-dev.yaml:**
```yaml
replicaCount: 1
autoscaling:
  enabled: false
config:
  tls:
    enabled: false
  auth:
    enabled: false
```

**values-prod.yaml:**
```yaml
replicaCount: 3
config:
  tls:
    enabled: true
    existingSecret: codec-server-tls
  auth:
    enabled: true
    existingSecret: codec-server-api-key
  s3:
    existingSecret: codec-server-s3-creds
monitoring:
  serviceMonitor:
    enabled: true
  prometheusRule:
    enabled: true
```

## Upgrading

```bash
helm upgrade codec-server ./helm/codec-server \
  --namespace temporal \
  --values values-prod.yaml
```

## Uninstallation

```bash
helm uninstall codec-server --namespace temporal
```
