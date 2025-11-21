# Kubernetes Deployment Guide

This directory contains Kubernetes manifests for deploying the large files processing solution to a Kubernetes cluster.

## Prerequisites

- Kubernetes cluster (EKS, GKE, AKS, or self-hosted)
- kubectl configured
- Container registry for Docker images
- S3 bucket created
- IAM roles configured (for AWS)

## Quick Start

### 1. Create Namespace

```bash
kubectl create namespace temporal
```

### 2. Build and Push Docker Images

```bash
# Codec Server
cd codec-server
docker build -t your-registry/temporal-codec-server:latest .
docker push your-registry/temporal-codec-server:latest

# Cleanup Worker
cd ../cleanup-worker
docker build -t your-registry/temporal-cleanup-worker:latest .
docker push your-registry/temporal-cleanup-worker:latest
```

### 3. Configure IAM (AWS only)

```bash
# Create IAM roles
aws iam create-role --role-name codec-server-role \
  --assume-role-policy-document file://k8s/trust-relationship.json

# Attach policies
aws iam put-role-policy --role-name codec-server-role \
  --policy-name CodecServerS3Access \
  --policy-document file://k8s/codec-server-policy.json
```

### 4. Update Manifests

Edit the following files and replace placeholders:

- `codec-server-deployment.yaml`:
  - `your-registry/temporal-codec-server:latest` → your image
  - `arn:aws:iam::ACCOUNT_ID:role/codec-server-role` → your IAM role ARN
  - `S3_BUCKET` → your S3 bucket name

- `cleanup-worker-deployment.yaml`:
  - `your-registry/temporal-cleanup-worker:latest` → your image
  - `arn:aws:iam::ACCOUNT_ID:role/cleanup-worker-role` → your IAM role ARN
  - `TEMPORAL_ADDRESS` → your Temporal server address

### 5. Deploy

```bash
# Deploy codec server
kubectl apply -f k8s/codec-server-deployment.yaml

# Deploy cleanup worker
kubectl apply -f k8s/cleanup-worker-deployment.yaml

# Verify deployments
kubectl get pods -n temporal
kubectl get svc -n temporal
```

## Configuration

### Codec Server

Edit `codec-server-config` ConfigMap:

```yaml
PAYLOAD_SIZE_THRESHOLD_BYTES: "2097152"  # 2MB
S3_BUCKET: "your-bucket-name"
S3_REGION: "us-east-1"
```

### Cleanup Worker

Edit `cleanup-worker-config` ConfigMap:

```yaml
CLEANUP_GRACE_PERIOD_DAYS: "7"
CHECK_ARCHIVE_BEFORE_DELETE: "true"
```

## Scaling

### Manual Scaling

```bash
kubectl scale deployment codec-server -n temporal --replicas=5
```

### Auto-Scaling

The HorizontalPodAutoscaler is configured to scale between 3-10 replicas based on:
- CPU utilization (70% target)
- Memory utilization (80% target)

Modify `codec-server-hpa` in the manifest to adjust:

```yaml
minReplicas: 3
maxReplicas: 10
```

## Monitoring

### Prometheus

The codec server exposes metrics at `/metrics` on port 8080.

Annotations are configured for Prometheus auto-discovery:
```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "8080"
prometheus.io/path: "/metrics"
```

### Key Metrics

- `codec_encode_requests_total` - Total encode requests
- `codec_s3_uploads_total` - Total S3 uploads
- `codec_avg_encode_latency_ms` - Average encoding latency
- `codec_bytes_uploaded_total` - Total bytes uploaded to S3

### Grafana Dashboard

Create a dashboard with queries:

```promql
# Request rate
rate(codec_encode_requests_total[5m])

# Error rate
rate(codec_encode_errors_total[5m]) / rate(codec_encode_requests_total[5m])

# Latency
codec_avg_encode_latency_ms

# S3 upload size
rate(codec_bytes_uploaded_total[5m])
```

## Security

### Network Policies

Create network policies to restrict traffic:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: codec-server-netpol
  namespace: temporal
spec:
  podSelector:
    matchLabels:
      app: codec-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: temporal-worker
      ports:
        - protocol: TCP
          port: 9090
        - protocol: TCP
          port: 8080
```

### Pod Security

All deployments include:
- `runAsNonRoot: true`
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- Dropped all capabilities

### Secrets Management

For sensitive configuration, use Kubernetes Secrets:

```bash
kubectl create secret generic codec-server-secrets \
  --from-literal=aws-access-key-id=YOUR_KEY \
  --from-literal=aws-secret-access-key=YOUR_SECRET \
  -n temporal
```

Then reference in deployment:
```yaml
env:
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: codec-server-secrets
        key: aws-access-key-id
```

**Note**: Prefer IAM roles over access keys in production.

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n temporal
kubectl describe pod codec-server-xxx -n temporal
```

### View Logs

```bash
kubectl logs -f deployment/codec-server -n temporal
kubectl logs -f deployment/cleanup-worker -n temporal
```

### Check Service

```bash
kubectl get svc codec-server -n temporal
kubectl port-forward svc/codec-server 8080:8080 -n temporal
curl http://localhost:8080/health
```

### Common Issues

**Pods not starting:**
- Check IAM role annotations
- Verify S3 bucket exists
- Check resource limits

**S3 access denied:**
- Verify IAM policy
- Check service account annotation
- Ensure IRSA is configured

**High memory usage:**
- Adjust `PAYLOAD_SIZE_THRESHOLD_BYTES`
- Increase memory limits
- Check for memory leaks in metrics

## Backup and Recovery

### ConfigMaps

```bash
# Backup
kubectl get configmap -n temporal -o yaml > backup-configmaps.yaml

# Restore
kubectl apply -f backup-configmaps.yaml
```

### Disaster Recovery

1. S3 data is persistent
2. Stateless codec server - just redeploy
3. Cleanup worker can be restarted safely

## Upgrades

### Rolling Update

```bash
# Update image
kubectl set image deployment/codec-server \
  codec-server=your-registry/temporal-codec-server:v2 \
  -n temporal

# Monitor rollout
kubectl rollout status deployment/codec-server -n temporal

# Rollback if needed
kubectl rollout undo deployment/codec-server -n temporal
```

### Zero-Downtime Deployment

The PodDisruptionBudget ensures at least 2 pods remain available during updates.

## Cost Optimization

1. **Right-size resources**: Monitor actual usage and adjust limits
2. **Use spot instances**: For cleanup worker (can tolerate interruptions)
3. **S3 lifecycle policies**: Move old data to cheaper storage classes
4. **Auto-scaling**: Scale down during low traffic

## Additional Resources

- [Temporal on Kubernetes](https://docs.temporal.io/docs/server/production-deployment)
- [AWS IAM Roles for Service Accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [Kubernetes Best Practices](https://kubernetes.io/docs/concepts/configuration/overview/)
