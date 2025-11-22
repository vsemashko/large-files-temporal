# Disaster Recovery Procedures

**Last Updated**: 2025-11-22
**Owner**: Platform Team
**Review Frequency**: Quarterly

## Table of Contents

- [Overview](#overview)
- [RTO/RPO Specifications](#rtorpo-specifications)
- [Backup Strategy](#backup-strategy)
- [Recovery Procedures](#recovery-procedures)
- [Testing Schedule](#testing-schedule)
- [Contact Information](#contact-information)

## Overview

This document outlines disaster recovery procedures for the Temporal Large Files Codec Server. It covers backup strategies, recovery procedures, and testing requirements to ensure business continuity.

### System Components

1. **Codec Server**: Stateless application (3+ replicas in production)
2. **S3 Bucket**: Primary storage for large payloads
3. **Temporal Workflows**: Workflow state managed by Temporal Server
4. **Configuration**: Secrets (TLS certs, API keys) in Kubernetes

## RTO/RPO Specifications

| Component | RTO (Recovery Time) | RPO (Recovery Point) | Notes |
|-----------|---------------------|----------------------|-------|
| Codec Server | 15 minutes | 0 (stateless) | Deploy new pods |
| S3 Payloads | 30 minutes | 5 minutes | Restore from versioning/backup |
| Configuration | 10 minutes | 0 | Re-apply from Git |
| Full System | 1 hour | 5 minutes | Complete rebuild |

**Definitions:**
- **RTO**: Maximum acceptable downtime
- **RPO**: Maximum acceptable data loss

## Backup Strategy

### 1. S3 Bucket Backup

#### Configuration

```bash
# Enable versioning (REQUIRED for production)
aws s3api put-bucket-versioning \
  --bucket temporal-large-payloads-prod \
  --versioning-configuration Status=Enabled

# Enable lifecycle policy for old versions
aws s3api put-bucket-lifecycle-configuration \
  --bucket temporal-large-payloads-prod \
  --lifecycle-configuration file://s3-lifecycle.json
```

**s3-lifecycle.json:**
```json
{
  "Rules": [
    {
      "Id": "archive-old-versions",
      "Status": "Enabled",
      "NoncurrentVersionTransitions": [
        {
          "NoncurrentDays": 30,
          "StorageClass": "GLACIER"
        }
      ],
      "NoncurrentVersionExpiration": {
        "NoncurrentDays": 90
      }
    },
    {
      "Id": "delete-incomplete-uploads",
      "Status": "Enabled",
      "AbortIncompleteMultipartUpload": {
        "DaysAfterInitiation": 7
      }
    }
  ]
}
```

#### Cross-Region Replication

```bash
# Create replication bucket in different region
aws s3 mb s3://temporal-large-payloads-prod-dr --region us-west-2

# Enable versioning on DR bucket
aws s3api put-bucket-versioning \
  --bucket temporal-large-payloads-prod-dr \
  --versioning-configuration Status=Enabled \
  --region us-west-2

# Set up replication (requires IAM role)
aws s3api put-bucket-replication \
  --bucket temporal-large-payloads-prod \
  --replication-configuration file://replication-config.json
```

**Replication Policy:**
- Primary: us-east-1
- DR: us-west-2
- Replication time: < 15 minutes (SLA)

### 2. Configuration Backup

All configuration is stored in Git:
- Kubernetes manifests: `k8s/`
- Helm charts: `helm/`
- Monitoring: `monitoring/`
- Scripts: `scripts/`

**Backup Frequency**: On every commit (automated via GitHub)

### 3. Secrets Backup

**CRITICAL**: Store secrets securely outside the cluster

Options:
1. **AWS Secrets Manager** (Recommended)
   ```bash
   # Backup API key
   aws secretsmanager create-secret \
     --name codec-server/api-key \
     --secret-string "$(kubectl get secret codec-server-api-key -n temporal -o jsonpath='{.data.api-key}' | base64 -d)"

   # Backup TLS certificate
   aws secretsmanager create-secret \
     --name codec-server/tls-cert \
     --secret-binary "$(kubectl get secret codec-server-tls -n temporal -o jsonpath='{.data.tls\.crt}')"
   ```

2. **HashiCorp Vault**
3. **Encrypted backup to S3**

### 4. Monitoring Data

- **Prometheus**: 15-day retention (configurable)
- **Grafana Dashboards**: Exported to Git (monitoring/)
- **Logs**: Retained for 30 days in CloudWatch/Loki

## Recovery Procedures

### Scenario 1: Codec Server Pod Failure

**Symptoms:**
- Pod in CrashLoopBackOff
- Health check failing
- High error rate in metrics

**Recovery Steps:**

```bash
# 1. Check pod status
kubectl get pods -n temporal -l app=codec-server

# 2. View pod logs
kubectl logs -n temporal <pod-name> --tail=100

# 3. Describe pod for events
kubectl describe pod -n temporal <pod-name>

# 4. Common fixes:
#    a. Restart pod
kubectl delete pod -n temporal <pod-name>

#    b. Rollback to previous version
kubectl rollout undo deployment/codec-server -n temporal

#    c. Check for resource constraints
kubectl top pods -n temporal -l app=codec-server

# 5. Verify recovery
kubectl rollout status deployment/codec-server -n temporal
curl http://codec-server:8080/health
```

**RTO**: 5-10 minutes

### Scenario 2: Complete Codec Server Failure

**Symptoms:**
- All pods down
- Deployment deleted
- Namespace corrupted

**Recovery Steps:**

```bash
# 1. Re-create namespace (if needed)
kubectl create namespace temporal

# 2. Re-create secrets
kubectl apply -f k8s/codec-server-secrets.yaml -n temporal

# 3. Redeploy using Helm
helm upgrade --install codec-server ./helm/codec-server \
  --namespace temporal \
  --values helm/codec-server/values-prod.yaml

# 4. Verify deployment
kubectl get pods -n temporal -l app=codec-server
kubectl get svc -n temporal codec-server

# 5. Run smoke tests
./scripts/test-large-payload.sh

# 6. Verify metrics
curl http://codec-server:8080/metrics | grep codec_
```

**RTO**: 15-30 minutes

### Scenario 3: S3 Bucket Unavailable

**Symptoms:**
- S3 upload/download errors
- High S3 error rate in metrics
- Workflows failing

**Recovery Steps:**

```bash
# 1. Check S3 service status
aws s3 ls s3://temporal-large-payloads-prod/

# 2. Verify bucket permissions
aws s3api get-bucket-policy \
  --bucket temporal-large-payloads-prod

# 3. Check IAM role/credentials
kubectl exec -it -n temporal <codec-pod> -- env | grep AWS

# 4. If primary bucket is down, failover to DR bucket:
#    Update config
kubectl set env deployment/codec-server \
  S3_BUCKET=temporal-large-payloads-prod-dr \
  S3_REGION=us-west-2 \
  -n temporal

# 5. Verify recovery
kubectl logs -f -n temporal -l app=codec-server
```

**RTO**: 10-15 minutes (with DR bucket)
**RTO**: 30-60 minutes (without DR bucket, depends on AWS)

### Scenario 4: Data Corruption in S3

**Symptoms:**
- Specific workflows failing to decode
- Corrupted payload errors
- Data integrity issues

**Recovery Steps:**

```bash
# 1. Identify affected objects
namespace="production"
workflow_id="workflow-123"
run_id="run-456"

# 2. List object versions
aws s3api list-object-versions \
  --bucket temporal-large-payloads-prod \
  --prefix "${namespace}/${workflow_id}/${run_id}/"

# 3. Restore from previous version
object_key="${namespace}/${workflow_id}/${run_id}/payload-hash"
version_id="<version-id-from-step-2>"

aws s3api get-object \
  --bucket temporal-large-payloads-prod \
  --key "${object_key}" \
  --version-id "${version_id}" \
  restored-payload.bin

# 4. Upload restored version
aws s3 cp restored-payload.bin \
  "s3://temporal-large-payloads-prod/${object_key}"

# 5. Verify workflow can now decode
# Test decode via codec server
```

**RTO**: 20-30 minutes per affected workflow
**RPO**: Depends on versioning retention (90 days)

### Scenario 5: Kubernetes Cluster Failure

**Symptoms:**
- Entire cluster unavailable
- Cannot access any Kubernetes resources

**Recovery Steps:**

```bash
# 1. Deploy to DR cluster or new cluster
export KUBECONFIG=~/.kube/config-dr

# 2. Create namespace
kubectl create namespace temporal

# 3. Restore secrets from AWS Secrets Manager
aws secretsmanager get-secret-value \
  --secret-id codec-server/api-key \
  --query SecretString --output text | \
kubectl create secret generic codec-server-api-key \
  --from-literal=api-key=- \
  -n temporal

# 4. Deploy codec server
helm install codec-server ./helm/codec-server \
  --namespace temporal \
  --values helm/codec-server/values-prod.yaml

# 5. Update DNS/Load Balancer to point to new cluster

# 6. Deploy monitoring
./scripts/deploy-monitoring.sh

# 7. Verify full functionality
./scripts/test-large-payload.sh
k6 run tests/load-test.js
```

**RTO**: 30-60 minutes (depends on cluster provisioning)

### Scenario 6: Lost TLS Certificates

**Symptoms:**
- Cannot start codec server
- TLS handshake failures
- Certificate expired

**Recovery Steps:**

```bash
# Option 1: Restore from AWS Secrets Manager
aws secretsmanager get-secret-value \
  --secret-id codec-server/tls-cert \
  --query SecretBinary --output text | base64 -d > tls.crt

aws secretsmanager get-secret-value \
  --secret-id codec-server/tls-key \
  --query SecretBinary --output text | base64 -d > tls.key

kubectl create secret tls codec-server-tls \
  --cert=tls.crt \
  --key=tls.key \
  -n temporal

# Option 2: Generate new self-signed cert (dev/staging only)
openssl req -x509 -newkey rsa:4096 \
  -keyout tls.key -out tls.crt \
  -days 365 -nodes \
  -subj "/CN=codec-server.temporal.svc.cluster.local"

kubectl create secret tls codec-server-tls \
  --cert=tls.crt \
  --key=tls.key \
  -n temporal

# Option 3: Use cert-manager to issue new cert
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: codec-server-tls
  namespace: temporal
spec:
  secretName: codec-server-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - codec-server.example.com
EOF

# 4. Restart pods to pick up new certificate
kubectl rollout restart deployment/codec-server -n temporal
```

**RTO**: 15-30 minutes

## Testing Schedule

### Monthly DR Drills

**First Monday of each month:**

1. **Pod Failure Test** (30 min)
   - Delete random pod
   - Verify auto-recovery
   - Check metrics for recovery time

2. **S3 Restore Test** (45 min)
   - Upload test payload
   - Delete object
   - Restore from version
   - Verify decode works

### Quarterly DR Tests

**First week of each quarter:**

1. **Complete Failover** (2 hours)
   - Simulate primary region failure
   - Failover to DR region
   - Run full test suite
   - Failback to primary

2. **Cluster Rebuild** (3 hours)
   - Deploy to fresh cluster
   - Restore all config and secrets
   - Run load tests
   - Verify performance SLAs

### Annual DR Validation

**Once per year:**

1. **Full Disaster Scenario** (4 hours)
   - Simulate catastrophic failure
   - Complete rebuild from documentation
   - Validate all runbooks
   - Update procedures

## Recovery Validation

After any recovery, perform:

1. **Health Check**
   ```bash
   curl http://codec-server:8080/health | jq
   ```

2. **Functional Test**
   ```bash
   ./scripts/test-large-payload.sh
   ```

3. **Load Test**
   ```bash
   k6 run --duration 5m --vus 50 tests/load-test.js
   ```

4. **Monitoring Check**
   - Verify all Prometheus alerts are green
   - Check Grafana dashboards
   - Review recent logs

## Backup Verification

**Weekly automated checks:**

```bash
#!/bin/bash
# verify-backups.sh

# Check S3 versioning is enabled
aws s3api get-bucket-versioning \
  --bucket temporal-large-payloads-prod

# Check cross-region replication
aws s3api get-bucket-replication \
  --bucket temporal-large-payloads-prod

# Verify secrets in Secrets Manager
aws secretsmanager describe-secret \
  --secret-id codec-server/api-key

# Test restore from version
# (actual test implementation)
```

## Contact Information

### Escalation Matrix

| Level | Contact | Response Time |
|-------|---------|---------------|
| L1 | On-call Engineer | 15 minutes |
| L2 | Platform Team Lead | 30 minutes |
| L3 | Infrastructure Architect | 1 hour |
| Executive | VP Engineering | 2 hours |

### Emergency Contacts

- **On-Call**: PagerDuty rotation
- **Slack**: #platform-incidents
- **Email**: platform-oncall@example.com

## Post-Incident Review

After any DR event, complete:

1. **Incident Report**
   - Timeline of events
   - Root cause analysis
   - Recovery actions taken
   - Lessons learned

2. **Runbook Updates**
   - Document new findings
   - Update procedures
   - Add automation where possible

3. **Training**
   - Share learnings with team
   - Update DR documentation
   - Schedule drill if needed

## Appendix

### S3 Access Patterns During DR

- Primary bucket: us-east-1 (normal operations)
- DR bucket: us-west-2 (failover only)
- Replication lag: < 15 minutes
- Consistency: Eventually consistent

### Cost Considerations

| Item | Monthly Cost (est.) |
|------|---------------------|
| S3 versioning | $50-200 |
| Cross-region replication | $100-500 |
| DR cluster (standby) | $0-2000 |
| Secrets Manager | $10-50 |

### Related Documentation

- [QUICKSTART.md](../QUICKSTART.md) - Deployment guide
- [TESTING.md](../TESTING.md) - Testing procedures
- [monitoring/README.md](../monitoring/README.md) - Monitoring setup
- [Runbooks](runbooks/) - Operational procedures
