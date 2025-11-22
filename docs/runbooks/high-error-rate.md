# Runbook: High Error Rate

**Alert**: `CodecHighErrorRate` / `CodecCriticalErrorRate`
**Severity**: Warning (>5%) / Critical (>20%)
**Team**: Platform

## Symptoms

- Prometheus alert firing: `CodecHighErrorRate`
- Error rate > 5% in Grafana dashboard
- Increased encode/decode errors in metrics
- Workflows failing with codec errors

## Impact

- **User Impact**: Workflows fail to encode/decode large payloads
- **Business Impact**: Temporal workflows cannot process large data
- **System Impact**: Increased S3 errors, possible cascade failures

## Triage (5 minutes)

### 1. Confirm the Alert

```bash
# Check current error rate
kubectl exec -n temporal $(kubectl get pod -n temporal -l app=codec-server -o jsonpath='{.items[0].metadata.name}') -- \
  wget -qO- http://localhost:8080/metrics | grep -E "codec_(encode|decode)_errors_total|codec_(encode|decode)_requests_total"

# Expected output:
# codec_encode_errors_total{} 0
# codec_encode_requests_total{} 1234
```

### 2. Check Recent Logs

```bash
# Get last 50 errors
kubectl logs -n temporal -l app=codec-server --tail=100 | grep -i error

# Common error patterns:
# - "S3 upload failed" → S3 connectivity issue
# - "failed to encode payloads" → Codec logic error
# - "context deadline exceeded" → Timeout issue
# - "invalid payload" → Client sending bad data
```

### 3. Identify Error Type

```bash
# Check error distribution in Grafana
# Go to Codec Server dashboard → "Top Error Messages" panel

# Or query Prometheus directly
curl -s 'http://prometheus:9090/api/v1/query?query=topk(5,sum(increase(codec_encode_errors_total[5m]))by(error_type))'
```

## Diagnosis (10 minutes)

### Root Cause 1: S3 Connectivity Issues

**Symptoms:**
- Errors contain "S3" or "AWS"
- S3 upload/download metrics show failures

**Diagnosis:**
```bash
# Check S3 connectivity from pod
kubectl exec -it -n temporal <codec-pod> -- sh
aws s3 ls s3://temporal-large-payloads-prod/

# Check IAM role/credentials
env | grep AWS

# Test S3 upload
echo "test" > /tmp/test.txt
aws s3 cp /tmp/test.txt s3://temporal-large-payloads-prod/health-check/
```

**Common Causes:**
- S3 service degradation (check AWS status)
- IAM role permissions changed
- Network policy blocking S3 access
- S3 bucket deleted or wrong region

### Root Cause 2: Payload Too Large

**Symptoms:**
- Errors about payload size
- Memory pressure on pods

**Diagnosis:**
```bash
# Check largest payload size
kubectl exec -n temporal <codec-pod> -- \
  wget -qO- http://localhost:8080/metrics | grep codec_largest_payload_bytes

# Check memory usage
kubectl top pods -n temporal -l app=codec-server
```

**Common Causes:**
- Client sending payloads > 100MB
- Memory limits too low
- Not enough disk space for temp files

### Root Cause 3: Rate Limiting

**Symptoms:**
- 429 errors in logs
- Errors correlated with traffic spikes

**Diagnosis:**
```bash
# Check rate limit metrics
kubectl exec -n temporal <codec-pod> -- \
  wget -qO- http://localhost:8080/metrics | grep rate_limit

# Check traffic spike in Grafana
# Look for correlation between error rate and request rate
```

**Common Causes:**
- Traffic spike exceeding rate limits
- Single client sending too many requests
- DDoS attack

### Root Cause 4: Invalid Payloads

**Symptoms:**
- "invalid payload" or "unmarshal" errors
- Errors started after client deployment

**Diagnosis:**
```bash
# Check recent client deployments
kubectl get events -n temporal --sort-by='.lastTimestamp'

# Review sample error payload
kubectl logs -n temporal -l app=codec-server | grep -A5 "invalid payload"
```

**Common Causes:**
- Client sent corrupted data
- Client using wrong protobuf version
- Metadata format mismatch

## Resolution

### Fix 1: S3 Connectivity

```bash
# 1. Verify S3 bucket exists and is accessible
aws s3 ls s3://temporal-large-payloads-prod/

# 2. Check IAM role permissions
aws iam get-role --role-name codec-server-role
aws iam get-role-policy --role-name codec-server-role --policy-name s3-access

# 3. If permissions missing, update IAM policy
kubectl apply -f k8s/iam-policies.yaml

# 4. Restart pods to pick up new credentials
kubectl rollout restart deployment/codec-server -n temporal

# 5. Verify recovery
kubectl logs -f -n temporal -l app=codec-server
```

**ETA**: 10-15 minutes

### Fix 2: Increase Memory Limits

```bash
# 1. Check current limits
kubectl get deployment codec-server -n temporal -o jsonpath='{.spec.template.spec.containers[0].resources}'

# 2. Increase memory limit
kubectl patch deployment codec-server -n temporal -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [{
          "name": "codec-server",
          "resources": {
            "limits": {"memory": "8Gi"},
            "requests": {"memory": "2Gi"}
          }
        }]
      }
    }
  }
}'

# 3. Monitor rollout
kubectl rollout status deployment/codec-server -n temporal
```

**ETA**: 5-10 minutes

### Fix 3: Adjust Rate Limits

```bash
# 1. Increase rate limits temporarily
kubectl set env deployment/codec-server \
  RATE_LIMIT_RPS=1000 \
  RATE_LIMIT_BURST=2000 \
  -n temporal

# 2. Or disable rate limiting (emergency only)
kubectl set env deployment/codec-server \
  RATE_LIMIT_ENABLED=false \
  -n temporal

# 3. Identify high-volume client
kubectl logs -n temporal -l app=codec-server | \
  grep -oP 'from \K[0-9.]+' | sort | uniq -c | sort -rn | head

# 4. Implement client-specific rate limiting (if needed)
```

**ETA**: 5 minutes

### Fix 4: Client Fix Required

```bash
# 1. Identify problematic client
# Check logs for client IP/workflowID patterns

# 2. Temporarily reject bad requests
kubectl patch deployment codec-server -n temporal --type json \
  -p '[{"op":"add","path":"/spec/template/spec/containers/0/env/-","value":{"name":"STRICT_VALIDATION","value":"true"}}]'

# 3. Contact client team to fix their code

# 4. Add validation in codec server (code change required)
```

**ETA**: 15 minutes (temporary fix) / 1-2 days (permanent fix)

## Verification

After applying fix:

```bash
# 1. Check error rate in metrics
kubectl exec -n temporal <codec-pod> -- \
  wget -qO- http://localhost:8080/metrics | grep codec_encode_errors_total

# 2. Verify Prometheus alert cleared
# Check Prometheus UI or Alertmanager

# 3. Run smoke test
./scripts/test-large-payload.sh

# 4. Monitor for 15 minutes
watch -n 30 'kubectl top pods -n temporal -l app=codec-server'
```

## Prevention

### Short-term (implement immediately)

1. **Increase monitoring granularity**
   ```bash
   # Add alert for specific error types
   # Update prometheus-rules.yaml with per-error-type alerts
   ```

2. **Add request validation**
   - Implement stricter payload size checks
   - Validate metadata format before processing

3. **Implement circuit breaker for S3**
   - Add retry logic with exponential backoff
   - Fail fast if S3 is down

### Long-term (next sprint)

1. **Improve error handling**
   - Better error messages to clients
   - Categorize errors (retriable vs non-retriable)

2. **Add client metrics**
   - Track errors per client/workflowID
   - Alert on per-client error rates

3. **Implement payload size quotas**
   - Limit max payload size per workflow
   - Add warnings for large payloads

## Escalation

If error rate doesn't improve after 30 minutes:

1. **Page Platform Lead**
   - Escalate via PagerDuty
   - Brief: Current error rate, attempted fixes

2. **Prepare for Rollback**
   ```bash
   # Rollback to previous version
   kubectl rollout undo deployment/codec-server -n temporal
   ```

3. **Enable Debug Logging**
   ```bash
   kubectl set env deployment/codec-server \
     LOG_LEVEL=debug \
     -n temporal
   ```

4. **Collect Diagnostics**
   ```bash
   # Save logs
   kubectl logs -n temporal -l app=codec-server --tail=1000 > codec-server-errors.log

   # Save metrics snapshot
   curl http://prometheus:9090/api/v1/query_range?query=codec_encode_errors_total > metrics.json

   # Create incident channel
   # Post diagnostics to #incident-codec-server
   ```

## Related Runbooks

- [codec-server-down.md](codec-server-down.md) - Complete service outage
- [s3-connectivity-issues.md](s3-connectivity-issues.md) - S3-specific problems
- [high-latency.md](high-latency.md) - Performance issues

## Incident History

| Date | Error Rate | Root Cause | Resolution | Time to Resolve |
|------|------------|------------|------------|-----------------|
| 2024-01-15 | 12% | S3 IAM permissions | Updated policy | 15 min |
| 2024-02-03 | 8% | Memory pressure | Increased limits | 10 min |
| 2024-03-20 | 25% | Client sending invalid data | Client hotfix | 2 hours |

## Post-Incident

After resolving:

1. **Update incident log** above
2. **File GitHub issue** if code change needed
3. **Document in Slack** #platform-incidents
4. **Schedule post-mortem** if severity was critical
5. **Update runbook** with new learnings
