# Runbook: Codec Server Down

**Alert**: `CodecServerDown` / `CodecServerQuorumLoss`
**Severity**: Critical
**Team**: Platform
**Response Time**: 15 minutes

## Symptoms

- Prometheus alert firing: `CodecServerDown` or `CodecServerQuorumLoss`
- All codec server pods unavailable
- Temporal workflows failing with connection errors
- Health endpoint unreachable

## Impact

- **User Impact**: HIGH - All workflows using large payloads fail
- **Business Impact**: CRITICAL - Temporal workflows blocked
- **System Impact**: Cascading failures in dependent services

## Immediate Actions (First 5 Minutes)

### 1. Confirm Outage

```bash
# Check pod status
kubectl get pods -n temporal -l app=codec-server

# Expected bad states:
# - CrashLoopBackOff
# - Error
# - No pods found
```

### 2. Check Recent Changes

```bash
# Check recent deployments
kubectl rollout history deployment/codec-server -n temporal

# Check recent config changes
kubectl describe deployment codec-server -n temporal | grep -A20 "Events:"

# Check recent Helm releases
helm history codec-server -n temporal
```

### 3. Quick Health Check

```bash
# Try to access health endpoint
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://codec-server.temporal:8080/health

# If succeeds: Issue is with Prometheus scraping
# If fails: Service truly down
```

## Diagnosis (Next 10 Minutes)

### Check 1: Pod-Level Issues

```bash
# Get pod status
kubectl get pods -n temporal -l app=codec-server -o wide

# For each failing pod:
kubectl logs -n temporal <pod-name> --tail=50
kubectl describe pod -n temporal <pod-name>

# Look for:
# - Image pull errors
# - Liveness/readiness probe failures
# - OOM kills
# - Resource quota exceeded
```

### Check 2: Configuration Issues

```bash
# Check environment variables
kubectl get deployment codec-server -n temporal -o jsonpath='{.spec.template.spec.containers[0].env}' | jq

# Check secrets exist
kubectl get secrets -n temporal | grep codec-server

# Check ConfigMap
kubectl get configmap -n temporal codec-server -o yaml
```

### Check 3: Resource Constraints

```bash
# Check resource usage
kubectl top pods -n temporal -l app=codec-server

# Check resource quotas
kubectl describe resourcequota -n temporal

# Check node resources
kubectl top nodes
```

### Check 4: Network Issues

```bash
# Check service
kubectl get svc -n temporal codec-server

# Check endpoints
kubectl get endpoints -n temporal codec-server

# Test connectivity from another pod
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  nc -zv codec-server.temporal 8080
```

## Resolution Procedures

### Fix 1: Restart Failed Pods

**When to use:** Pods in CrashLoopBackOff, random failures

```bash
# Delete all pods (they will be recreated)
kubectl delete pods -n temporal -l app=codec-server

# Watch recreation
kubectl get pods -n temporal -l app=codec-server -w

# Check logs of new pods
kubectl logs -f -n temporal -l app=codec-server
```

**ETA:** 2-5 minutes

### Fix 2: Rollback Recent Deployment

**When to use:** Issues started after recent deployment

```bash
# Check recent rollout history
kubectl rollout history deployment/codec-server -n temporal

# Rollback to previous version
kubectl rollout undo deployment/codec-server -n temporal

# Watch rollback
kubectl rollout status deployment/codec-server -n temporal

# Verify rollback
kubectl get deployment codec-server -n temporal -o jsonpath='{.spec.template.spec.containers[0].image}'
```

**ETA:** 3-5 minutes

### Fix 3: Scale Up Manually

**When to use:** Autoscaler issues, need immediate capacity

```bash
# Check current replicas
kubectl get deployment codec-server -n temporal

# Scale up
kubectl scale deployment codec-server --replicas=5 -n temporal

# Verify scaling
kubectl get pods -n temporal -l app=codec-server -w
```

**ETA:** 1-2 minutes

### Fix 4: Fix Missing Secrets

**When to use:** Pods failing with secret mount errors

```bash
# Check if secrets exist
kubectl get secrets -n temporal | grep codec-server

# Recreate TLS secret (if missing)
kubectl create secret tls codec-server-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key \
  -n temporal

# Recreate API key secret (if missing)
kubectl create secret generic codec-server-api-key \
  --from-literal=api-key=<your-api-key> \
  -n temporal

# Restart pods to pick up secrets
kubectl rollout restart deployment/codec-server -n temporal
```

**ETA:** 5 minutes

### Fix 5: Fix Image Pull Errors

**When to use:** ImagePullBackOff errors

```bash
# Check image name
kubectl get deployment codec-server -n temporal -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check image exists
docker pull <image-name>

# If image doesn't exist:
# Option 1: Use previous known good version
kubectl set image deployment/codec-server \
  codec-server=ghcr.io/vsemashko/large-files-temporal/codec-server:v1.0.0 \
  -n temporal

# Option 2: Build and push image
cd codec-server
docker build -t ghcr.io/vsemashko/large-files-temporal/codec-server:latest .
docker push ghcr.io/vsemashko/large-files-temporal/codec-server:latest
```

**ETA:** 10-15 minutes

### Fix 6: Increase Resource Limits

**When to use:** OOMKilled errors, CPU throttling

```bash
# Increase memory and CPU limits
kubectl patch deployment codec-server -n temporal -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [{
          "name": "codec-server",
          "resources": {
            "limits": {
              "cpu": "4000m",
              "memory": "8Gi"
            },
            "requests": {
              "cpu": "1000m",
              "memory": "2Gi"
            }
          }
        }]
      }
    }
  }
}'

# Monitor rollout
kubectl rollout status deployment/codec-server -n temporal
```

**ETA:** 5 minutes

### Fix 7: Redeploy from Scratch

**When to use:** All else fails, complete corruption

```bash
# 1. Delete deployment
kubectl delete deployment codec-server -n temporal

# 2. Wait 30 seconds for cleanup
sleep 30

# 3. Redeploy using Helm
helm upgrade --install codec-server ./helm/codec-server \
  --namespace temporal \
  --values helm/codec-server/values-prod.yaml \
  --timeout 10m

# 4. Watch deployment
kubectl get pods -n temporal -l app=codec-server -w
```

**ETA:** 5-10 minutes

## Verification

```bash
# 1. All pods running
kubectl get pods -n temporal -l app=codec-server
# Expected: All Running (3/3 or more)

# 2. Health check passes
kubectl exec -n temporal $(kubectl get pod -n temporal -l app=codec-server -o jsonpath='{.items[0].metadata.name}') -- \
  wget -qO- http://localhost:8080/health | jq
# Expected: {"status":"healthy"}

# 3. Metrics endpoint works
kubectl exec -n temporal <pod-name> -- \
  wget -qO- http://localhost:8080/metrics | head -20

# 4. Service has endpoints
kubectl get endpoints -n temporal codec-server
# Expected: Multiple IPs listed

# 5. Run functional test
./scripts/test-large-payload.sh
# Expected: All tests pass

# 6. Prometheus alerts cleared
# Check Prometheus UI - alert should be resolved

# 7. Monitor for 15 minutes
kubectl top pods -n temporal -l app=codec-server
watch -n 10 'kubectl get pods -n temporal -l app=codec-server'
```

## Communication Template

### Initial Alert (within 5 minutes)

```
🚨 INCIDENT: Codec Server Down

Status: Investigating
Severity: Critical
Start Time: [TIMESTAMP]
Impact: All Temporal workflows using large payloads are failing

Actions Taken:
- Checked pod status: [RESULT]
- Reviewing recent changes: [RESULT]
- Current hypothesis: [HYPOTHESIS]

Next Steps:
- [ACTION 1]
- [ACTION 2]

Updates in: #platform-incidents
Point person: @oncall-engineer
```

### Resolution Announcement

```
✅ RESOLVED: Codec Server Down

Status: Resolved
Duration: [DURATION]
Root Cause: [CAUSE]

Resolution:
- [ACTION TAKEN]
- [VERIFICATION STEPS]

Impact:
- Workflows failed during outage: ~[NUMBER]
- Data loss: None
- Service restored at: [TIMESTAMP]

Post-mortem: [LINK]
Follow-up actions: [GITHUB ISSUE]
```

## Escalation Path

**If not resolved in 15 minutes:**

1. **Page Platform Lead**
   - PagerDuty: Platform-Oncall
   - Phone: [EMERGENCY NUMBER]

2. **Open War Room**
   - Slack: #incident-codec-server
   - Zoom: [INCIDENT BRIDGE]

3. **Notify Stakeholders**
   - Engineering Manager
   - On-call SRE
   - Customer Success (if customer-facing impact)

4. **Prepare Workaround**
   - Document manual payload handling
   - Increase Temporal workflow timeout
   - Route workflows to alternative cluster

## Prevention

### Immediate (within 24 hours)

1. **Add Canary Deployment**
   ```yaml
   # Use gradual rollout
   strategy:
     type: RollingUpdate
     rollingUpdate:
       maxUnavailable: 1
       maxSurge: 1
   ```

2. **Improve Health Checks**
   - Add dependency checks (S3 connectivity)
   - Increase probe timeout
   - Add startup probe for slow starts

3. **Implement PDB Correctly**
   ```bash
   kubectl apply -f helm/codec-server/templates/poddisruptionbudget.yaml
   ```

### Short-term (within 1 week)

1. **Add Pre-deployment Validation**
   - CI pipeline validation
   - Staging deployment required
   - Automated smoke tests

2. **Improve Monitoring**
   - Add synthetic checks
   - Monitor from multiple regions
   - Alert on deployment failures

3. **Document Common Failures**
   - Update runbooks
   - Create failure decision tree
   - Train on-call rotation

### Long-term (next quarter)

1. **Multi-Region Deployment**
   - Deploy to us-west-2 as DR
   - Implement automatic failover
   - Cross-region health checks

2. **Chaos Engineering**
   - Regular failure injection
   - Automated recovery testing
   - Game day exercises

3. **Self-Healing**
   - Automatic rollback on health check failures
   - Auto-scaling based on error rates
   - Circuit breaker for dependencies

## Post-Incident

**Required within 48 hours:**

1. **Write Post-Mortem**
   - Timeline of events
   - Root cause analysis
   - Action items with owners

2. **Update Runbook**
   - Add this incident to history
   - Document new learnings
   - Update resolution procedures

3. **File GitHub Issues**
   - For each prevention action
   - Assign owners and due dates
   - Link to post-mortem

## Related Runbooks

- [high-error-rate.md](high-error-rate.md) - High error rate issues
- [high-latency.md](high-latency.md) - Performance degradation
- [s3-connectivity-issues.md](s3-connectivity-issues.md) - S3 problems

## Incident History

| Date | Duration | Root Cause | Resolution | Lessons Learned |
|------|----------|------------|------------|-----------------|
| - | - | - | - | - |

_Update after each incident_
