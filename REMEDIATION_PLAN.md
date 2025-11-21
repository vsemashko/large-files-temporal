# Production Readiness Remediation Plan

## Overview

This plan addresses all critical and high-priority issues found in the code review to make the large-files-temporal project production-ready.

## Priority Levels

- **P0 (Critical)**: Must fix - blocks production deployment
- **P1 (High)**: Should fix - significant risk in production
- **P2 (Medium)**: Recommended - improves reliability and maintainability
- **P3 (Low)**: Nice to have - minor improvements

---

## Phase 1: Critical Bug Fixes (P0)

**Timeline**: Day 1-2
**Status**: 🔴 Blocking

### 1.1 Register Metrics Endpoint

**Issue**: Metrics endpoint handler exists but is not registered in HTTP mux.

**Files to modify**:
- `codec-server/cmd/server/main.go`

**Implementation**:
```go
// Add line 79 in main.go
httpMux.HandleFunc("/metrics", httpSrv.HandleMetrics)
```

**Testing**:
```bash
curl http://localhost:8080/metrics
# Should return Prometheus metrics, not 404
```

**Estimated time**: 5 minutes

---

### 1.2 Implement Metrics Instrumentation

**Issue**: Metrics recording functions exist but are never called.

**Files to modify**:
- `codec-server/internal/codec/codec.go`
- `codec-server/internal/storage/s3.go`
- `codec-server/internal/grpc/server.go`

**Implementation**:

1. **In `codec.go`** - Add timing and recording:
```go
func (c *Codec) Encode(ctx context.Context, payloads []*common.Payload, ...) ([]*common.Payload, error) {
    start := time.Now()
    defer func() {
        metrics.GetMetrics().RecordEncode(len(payloads), time.Since(start), err != nil)
    }()
    // ... existing code
}
```

2. **In `s3.go`** - Track S3 operations:
```go
func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, ...) error {
    start := time.Now()

    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{...})

    metrics.GetMetrics().RecordS3Upload(int64(len(data)), time.Since(start), err != nil)
    return err
}
```

3. **In `grpc/server.go`** - Already has defer for encode/decode

**Testing**:
```bash
# Generate load
./client

# Check metrics
curl http://localhost:8080/metrics | grep codec_
# Should show non-zero values
```

**Estimated time**: 2 hours

---

### 1.3 Fix S3 Pagination

**Issue**: ListObjects only returns first 1000 objects.

**Files to modify**:
- `codec-server/internal/storage/s3.go`
- `cleanup-worker/internal/storage/s3.go`

**Implementation**:
```go
func (s *S3Storage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
    var allKeys []string
    var continuationToken *string

    for {
        input := &s3.ListObjectsV2Input{
            Bucket:            aws.String(s.bucket),
            Prefix:            aws.String(prefix),
            ContinuationToken: continuationToken,
        }

        result, err := s.client.ListObjectsV2(ctx, input)
        if err != nil {
            return nil, fmt.Errorf("failed to list objects: %w", err)
        }

        for _, obj := range result.Contents {
            if obj.Key != nil {
                allKeys = append(allKeys, *obj.Key)
            }
        }

        if !aws.ToBool(result.IsTruncated) {
            break
        }
        continuationToken = result.NextContinuationToken
    }

    return allKeys, nil
}
```

**Testing**:
```bash
# Create 1500+ test objects in S3
# Run cleanup
# Verify all objects are listed and deleted
```

**Estimated time**: 1 hour

---

### 1.4 Fix Hard-coded Workflow Metadata in Remote Codec

**Issue**: Remote codec sends "unknown" for workflowID and runID.

**Files to modify**:
- `samples/go-worker/internal/codec/remote.go`
- `samples/typescript-worker/src/codec/remote-codec.ts`

**Implementation**:

**Option A**: Extract from Temporal context (requires interceptor)
**Option B**: Accept metadata in Encode/Decode methods (simpler)

We'll implement **Option B** for now:

```go
// Add workflow context extraction
type PayloadCodecWithContext interface {
    Encode(payloads []*common.Payload) ([]*common.Payload, error)
    Decode(payloads []*common.Payload) ([]*common.Payload, error)
    EncodeWithContext(ctx context.Context, payloads []*common.Payload) ([]*common.Payload, error)
}

// In Encode method, try to extract workflow info from context
func (r *RemotePayloadCodec) Encode(payloads []*common.Payload) ([]*common.Payload, error) {
    // Try to get workflow info from Temporal internal context
    // For now, we'll use a workaround with workflow interceptor
    namespace := "default"
    workflowID := "unknown"
    runID := "unknown"

    // TODO: Implement proper context extraction via interceptor

    resp, err := r.client.Encode(context.Background(), &pb.EncodeRequest{
        Payloads:   payloadBytes,
        Namespace:  namespace,
        WorkflowId: workflowID,
        RunId:      runID,
    })
    // ...
}
```

**Better solution**: Add workflow interceptor:
```go
type CodecInterceptor struct {
    codec PayloadCodec
}

func (i *CodecInterceptor) ExecuteWorkflow(ctx workflow.Context, ...) {
    info := workflow.GetInfo(ctx)
    // Store in context for codec to access
    ctx = context.WithValue(ctx, "temporal-workflow-id", info.WorkflowExecution.ID)
    ctx = context.WithValue(ctx, "temporal-run-id", info.WorkflowExecution.RunID)
    // ...
}
```

**Estimated time**: 3 hours

---

### 1.5 Add Request Size Limits

**Issue**: No limits on request body size, can cause OOM.

**Files to modify**:
- `codec-server/internal/http/server.go`
- `codec-server/cmd/server/main.go`

**Implementation**:

1. **Add max request size constant**:
```go
const (
    MaxRequestBodySize = 100 * 1024 * 1024 // 100MB
)
```

2. **Use http.MaxBytesReader**:
```go
func (s *Server) HandleEncode(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

    body, err := io.ReadAll(r.Body)
    if err != nil {
        if err.Error() == "http: request body too large" {
            http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
            return
        }
        // ... other error handling
    }
    // ...
}
```

3. **Add to server config**:
```go
httpServer := &nethttp.Server{
    Addr:           httpAddr,
    Handler:        httpMux,
    MaxHeaderBytes: 1 << 20, // 1MB headers
}
```

4. **Add to gRPC server**:
```go
grpcServer := grpc.NewServer(
    grpc.MaxRecvMsgSize(100 * 1024 * 1024), // 100MB
    grpc.MaxSendMsgSize(100 * 1024 * 1024), // 100MB
)
```

**Testing**:
```bash
# Send 101MB request
curl -X POST -d @large-file.json http://localhost:8080/encode
# Should return 413 Request Entity Too Large
```

**Estimated time**: 1 hour

---

### 1.6 Fix Archive Check Logic

**Issue**: Current logic incorrectly assumes all workflows with history are archived.

**Files to modify**:
- `cleanup-worker/internal/activities/visibility.go`

**Implementation**:
```go
func (a *VisibilityActivities) CheckIfArchived(ctx context.Context, wf workflows.WorkflowInfo) (bool, error) {
    logger := activity.GetLogger(ctx)

    // Check if archival is enabled for the namespace
    resp, err := a.client.DescribeNamespace(ctx, wf.Namespace)
    if err != nil {
        return false, fmt.Errorf("failed to describe namespace: %w", err)
    }

    // If archival is not enabled, return false
    if resp.Config.HistoryArchivalState != enums.ARCHIVAL_STATE_ENABLED {
        logger.Info("Archival not enabled for namespace", "namespace", wf.Namespace)
        return false, nil
    }

    // For now, if archival is enabled and workflow is closed,
    // assume it will be/has been archived
    // TODO: Implement actual archive query when Temporal supports it
    logger.Info("Archival enabled, assuming workflow is archived", "workflow", wf.WorkflowID)
    return true, nil
}
```

**Note**: Temporal's archival system automatically archives workflows based on retention policy. We should:
1. Check if archival is enabled
2. If CHECK_ARCHIVE_BEFORE_DELETE=true and archival is enabled, skip cleanup
3. If archival is disabled, proceed with cleanup after grace period

**Estimated time**: 1 hour

---

## Phase 2: Security Fixes (P0)

**Timeline**: Day 2-3
**Status**: 🔴 Blocking

### 2.1 Add TLS Support

**Issue**: All communication is unencrypted.

**Files to modify**:
- `codec-server/cmd/server/main.go`
- `samples/go-worker/internal/codec/remote.go`

**Implementation**:

1. **Generate self-signed certs for development**:
```bash
# Add to scripts/setup.sh
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
```

2. **Add TLS to gRPC server**:
```go
// Load TLS certificates
creds, err := credentials.NewServerTLSFromFile("cert.pem", "key.pem")
if err != nil {
    log.Fatalf("Failed to load TLS credentials: %v", err)
}

grpcServer := grpc.NewServer(
    grpc.Creds(creds),
    grpc.MaxRecvMsgSize(100 * 1024 * 1024),
    grpc.MaxSendMsgSize(100 * 1024 * 1024),
)
```

3. **Add TLS to HTTP server**:
```go
// Start HTTP server with TLS
go func() {
    if err := httpServer.ListenAndServeTLS("cert.pem", "key.pem"); err != nil && err != nethttp.ErrServerClosed {
        log.Fatalf("Failed to serve HTTPS: %v", err)
    }
}()
```

4. **Update client to use TLS**:
```go
// In remote codec
creds, err := credentials.NewClientTLSFromFile("cert.pem", "")
if err != nil {
    return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
}

conn, err := grpc.Dial(codecServerAddr, grpc.WithTransportCredentials(creds))
```

**Configuration**:
```env
CODEC_TLS_ENABLED=true
CODEC_TLS_CERT_FILE=/etc/codec/tls/cert.pem
CODEC_TLS_KEY_FILE=/etc/codec/tls/key.pem
CODEC_TLS_SKIP_VERIFY=false  # Only for development
```

**Estimated time**: 3 hours

---

### 2.2 Add Basic Authentication

**Issue**: No authentication on endpoints.

**Files to modify**:
- `codec-server/internal/http/server.go`
- `codec-server/internal/grpc/server.go`
- `codec-server/internal/config/config.go`

**Implementation**:

1. **Add API key middleware for HTTP**:
```go
type AuthMiddleware struct {
    apiKey string
}

func (a *AuthMiddleware) Middleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Skip auth for health endpoint
        if r.URL.Path == "/health" {
            next(w, r)
            return
        }

        apiKey := r.Header.Get("X-API-Key")
        if apiKey == "" {
            apiKey = r.Header.Get("Authorization")
            if strings.HasPrefix(apiKey, "Bearer ") {
                apiKey = strings.TrimPrefix(apiKey, "Bearer ")
            }
        }

        if apiKey != a.apiKey {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        next(w, r)
    }
}
```

2. **Add gRPC auth interceptor**:
```go
func authInterceptor(apiKey string) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        md, ok := metadata.FromIncomingContext(ctx)
        if !ok {
            return nil, status.Error(codes.Unauthenticated, "missing metadata")
        }

        keys := md.Get("authorization")
        if len(keys) == 0 {
            return nil, status.Error(codes.Unauthenticated, "missing api key")
        }

        if keys[0] != "Bearer "+apiKey {
            return nil, status.Error(codes.Unauthenticated, "invalid api key")
        }

        return handler(ctx, req)
    }
}
```

3. **Configuration**:
```go
type Config struct {
    // ... existing fields
    APIKey string  // Read from env or secret
}
```

**For production**: Use proper auth service (OAuth2, JWT, mTLS)

**Estimated time**: 2 hours

---

### 2.3 Add Rate Limiting

**Issue**: No protection against abuse.

**Files to modify**:
- `codec-server/internal/http/server.go`
- Add new file: `codec-server/internal/ratelimit/ratelimit.go`

**Implementation**:

1. **Create rate limiter**:
```go
package ratelimit

import (
    "golang.org/x/time/rate"
    "sync"
)

type IPRateLimiter struct {
    ips map[string]*rate.Limiter
    mu  *sync.RWMutex
    r   rate.Limit
    b   int
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
    return &IPRateLimiter{
        ips: make(map[string]*rate.Limiter),
        mu:  &sync.RWMutex{},
        r:   r,
        b:   b,
    }
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
    i.mu.Lock()
    defer i.mu.Unlock()

    limiter, exists := i.ips[ip]
    if !exists {
        limiter = rate.NewLimiter(i.r, i.b)
        i.ips[ip] = limiter
    }

    return limiter
}
```

2. **Add middleware**:
```go
func rateLimitMiddleware(limiter *IPRateLimiter) func(http.HandlerFunc) http.HandlerFunc {
    return func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            ip := getIP(r)
            limiter := limiter.GetLimiter(ip)

            if !limiter.Allow() {
                http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
                return
            }

            next(w, r)
        }
    }
}
```

**Configuration**:
```env
RATE_LIMIT_RPS=100
RATE_LIMIT_BURST=200
```

**Estimated time**: 2 hours

---

### 2.4 Add Input Validation

**Issue**: No validation on user inputs.

**Files to modify**:
- `codec-server/internal/http/server.go`
- Add new file: `codec-server/internal/validation/validation.go`

**Implementation**:
```go
package validation

import (
    "fmt"
    "regexp"
)

var (
    namespaceRegex  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
    workflowIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
    runIDRegex      = regexp.MustCompile(`^[a-f0-9-]+$`)
)

func ValidateNamespace(namespace string) error {
    if namespace == "" {
        return fmt.Errorf("namespace cannot be empty")
    }
    if len(namespace) > 200 {
        return fmt.Errorf("namespace too long")
    }
    if !namespaceRegex.MatchString(namespace) {
        return fmt.Errorf("namespace contains invalid characters")
    }
    return nil
}

func ValidateWorkflowID(workflowID string) error {
    if workflowID == "" || workflowID == "unknown" {
        return fmt.Errorf("workflowID cannot be empty or 'unknown'")
    }
    if len(workflowID) > 1000 {
        return fmt.Errorf("workflowID too long")
    }
    if !workflowIDRegex.MatchString(workflowID) {
        return fmt.Errorf("workflowID contains invalid characters")
    }
    return nil
}

func ValidateEncodeRequest(req EncodeRequest) error {
    if err := ValidateNamespace(req.Namespace); err != nil {
        return fmt.Errorf("invalid namespace: %w", err)
    }
    if err := ValidateWorkflowID(req.WorkflowID); err != nil {
        return fmt.Errorf("invalid workflowID: %w", err)
    }
    if len(req.Payloads) == 0 {
        return fmt.Errorf("no payloads provided")
    }
    if len(req.Payloads) > 1000 {
        return fmt.Errorf("too many payloads")
    }
    return nil
}
```

**Estimated time**: 1 hour

---

## Phase 3: Performance & Reliability (P1)

**Timeline**: Day 3-4
**Status**: 🟡 High Priority

### 3.1 Add HTTP/gRPC Timeouts

**Files to modify**:
- `codec-server/cmd/server/main.go`

**Implementation**:
```go
httpServer := &nethttp.Server{
    Addr:              httpAddr,
    Handler:           httpMux,
    MaxHeaderBytes:    1 << 20,
    ReadTimeout:       30 * time.Second,
    ReadHeaderTimeout: 10 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       120 * time.Second,
}

grpcServer := grpc.NewServer(
    grpc.MaxRecvMsgSize(100 * 1024 * 1024),
    grpc.MaxSendMsgSize(100 * 1024 * 1024),
    grpc.ConnectionTimeout(30 * time.Second),
    grpc.KeepaliveParams(keepalive.ServerParameters{
        MaxConnectionIdle:     15 * time.Minute,
        MaxConnectionAge:      30 * time.Minute,
        MaxConnectionAgeGrace: 5 * time.Minute,
        Time:                  5 * time.Minute,
        Timeout:               1 * time.Minute,
    }),
)
```

**Estimated time**: 30 minutes

---

### 3.2 Add S3 Operation Timeouts

**Files to modify**:
- `codec-server/internal/codec/codec.go`
- `codec-server/internal/storage/s3.go`

**Implementation**:
```go
func (c *Codec) storePayload(ctx context.Context, ...) (*common.Payload, error) {
    // Add timeout for S3 operations
    uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    if err := c.storage.Upload(uploadCtx, key, data, metadata); err != nil {
        return nil, fmt.Errorf("failed to upload to S3: %w", err)
    }
    // ...
}
```

**Estimated time**: 30 minutes

---

### 3.3 Implement Proper Health Checks

**Files to modify**:
- `codec-server/internal/http/server.go`

**Implementation**:
```go
type HealthChecker struct {
    storage storage.Storage
}

func (h *HealthChecker) Check(ctx context.Context) error {
    // Check S3 connectivity
    checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    // Try to list bucket
    _, err := h.storage.ListObjects(checkCtx, "")
    if err != nil {
        return fmt.Errorf("S3 unhealthy: %w", err)
    }

    return nil
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    err := s.healthChecker.Check(ctx)
    if err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{
            "status": "unhealthy",
            "error":  err.Error(),
        })
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status": "healthy",
    })
}
```

**Estimated time**: 1 hour

---

### 3.4 Add Configuration Validation

**Files to modify**:
- `codec-server/internal/config/config.go`
- `cleanup-worker/internal/config/config.go`

**Implementation**:
```go
func (c *Config) Validate() error {
    var errs []error

    if c.GRPCPort < 1 || c.GRPCPort > 65535 {
        errs = append(errs, fmt.Errorf("invalid gRPC port: %d", c.GRPCPort))
    }

    if c.HTTPPort < 1 || c.HTTPPort > 65535 {
        errs = append(errs, fmt.Errorf("invalid HTTP port: %d", c.HTTPPort))
    }

    if c.PayloadSizeThreshold <= 0 {
        errs = append(errs, fmt.Errorf("payload size threshold must be > 0"))
    }

    if c.PayloadSizeThreshold >= 4*1024*1024 {
        errs = append(errs, fmt.Errorf("payload size threshold must be < 4MB (Temporal limit)"))
    }

    if c.S3Bucket == "" {
        errs = append(errs, fmt.Errorf("S3 bucket cannot be empty"))
    }

    if c.S3Region == "" {
        errs = append(errs, fmt.Errorf("S3 region cannot be empty"))
    }

    if len(errs) > 0 {
        return fmt.Errorf("configuration validation failed: %v", errs)
    }

    return nil
}

func LoadConfig() (*Config, error) {
    cfg := &Config{...}

    if err := cfg.Validate(); err != nil {
        return nil, err
    }

    return cfg, nil
}
```

**Estimated time**: 1 hour

---

### 3.5 Add Graceful Shutdown Timeout

**Files to modify**:
- `codec-server/cmd/server/main.go`

**Implementation**:
```go
// Wait for interrupt signal
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
<-sigChan

log.Println("Shutting down gracefully...")

// Create shutdown context with timeout
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer shutdownCancel()

// Shutdown HTTP server
if err := httpServer.Shutdown(shutdownCtx); err != nil {
    log.Printf("HTTP server shutdown error: %v", err)
}

// Shutdown gRPC server
grpcServer.GracefulStop()

log.Println("Server stopped")
```

**Estimated time**: 15 minutes

---

## Phase 4: Testing & Observability (P1)

**Timeline**: Day 4-5
**Status**: 🟡 High Priority

### 4.1 Add Unit Tests

**Files to create**:
- `codec-server/internal/codec/codec_test.go`
- `codec-server/internal/storage/s3_test.go`
- `codec-server/internal/http/server_test.go`

**Implementation**:
```go
// codec_test.go
func TestEncode_SmallPayload(t *testing.T) {
    storage := &mockStorage{}
    codec := NewCodec(storage, 2*1024*1024, "test-bucket")

    payload := &common.Payload{
        Data: []byte("small"),
    }

    encoded, err := codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf-1", "run-1")
    assert.NoError(t, err)
    assert.Equal(t, payload, encoded[0], "Small payload should not be modified")
    assert.Equal(t, 0, storage.uploadCount, "Should not upload to S3")
}

func TestEncode_LargePayload(t *testing.T) {
    storage := &mockStorage{}
    codec := NewCodec(storage, 1024, "test-bucket")  // 1KB threshold

    payload := &common.Payload{
        Data: make([]byte, 2048),  // 2KB
    }

    encoded, err := codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf-1", "run-1")
    assert.NoError(t, err)
    assert.NotEqual(t, payload, encoded[0], "Large payload should be replaced with reference")
    assert.Equal(t, 1, storage.uploadCount, "Should upload to S3")

    // Verify it's an S3 reference
    assert.True(t, isS3Reference(encoded[0]))
}
```

**Coverage target**: >70% for critical paths

**Estimated time**: 8 hours

---

### 4.2 Add Integration Tests

**Files to create**:
- `tests/integration/codec_test.go`
- `tests/integration/cleanup_test.go`

**Implementation**:
```go
func TestIntegration_EncodeAndDecode(t *testing.T) {
    // Start LocalStack
    // Start codec server
    // Create large payload
    // Encode via HTTP
    // Verify S3 upload
    // Decode via HTTP
    // Verify payload matches
}
```

**Estimated time**: 4 hours

---

### 4.3 Add Distributed Tracing

**Files to modify**:
- Add new package: `codec-server/internal/tracing/`
- `codec-server/cmd/server/main.go`

**Implementation**:
```go
// Use OpenTelemetry
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

// Add to main.go
tp := initTracer()
defer tp.Shutdown(context.Background())

// Add to handlers
func (s *Server) HandleEncode(w http.ResponseWriter, r *http.Request) {
    ctx, span := otel.Tracer("codec-server").Start(r.Context(), "encode")
    defer span.End()

    // Add attributes
    span.SetAttributes(
        attribute.String("namespace", req.Namespace),
        attribute.String("workflow.id", req.WorkflowID),
    )

    // ... existing code
}
```

**Estimated time**: 4 hours

---

## Phase 5: Documentation & Deployment (P2)

**Timeline**: Day 5-6
**Status**: 🟢 Recommended

### 5.1 Update Documentation

**Files to modify**:
- `README.md` - Add security section
- `TESTING.md` - Add new test procedures
- `k8s/README.md` - Update with TLS and auth setup

**Estimated time**: 2 hours

---

### 5.2 Update Kubernetes Manifests

**Files to modify**:
- `k8s/codec-server-deployment.yaml`
- Add: `k8s/secrets.yaml`
- Add: `k8s/tls-config.yaml`

**Implementation**:
```yaml
# Add TLS secret
apiVersion: v1
kind: Secret
metadata:
  name: codec-server-tls
  namespace: temporal
type: kubernetes.io/tls
data:
  tls.crt: <base64-cert>
  tls.key: <base64-key>

---
# Add API key secret
apiVersion: v1
kind: Secret
metadata:
  name: codec-server-api-key
  namespace: temporal
type: Opaque
data:
  api-key: <base64-api-key>

---
# Update deployment to use secrets
env:
  - name: CODEC_API_KEY
    valueFrom:
      secretKeyRef:
        name: codec-server-api-key
        key: api-key
volumeMounts:
  - name: tls
    mountPath: /etc/codec/tls
    readOnly: true
volumes:
  - name: tls
    secret:
      secretName: codec-server-tls
```

**Estimated time**: 2 hours

---

### 5.3 Create Runbooks

**Files to create**:
- `docs/runbooks/incident-response.md`
- `docs/runbooks/deployment.md`
- `docs/runbooks/rollback.md`

**Estimated time**: 3 hours

---

## Summary

### Total Estimated Time
- **Phase 1 (Critical Bugs)**: 8.75 hours
- **Phase 2 (Security)**: 10 hours
- **Phase 3 (Performance)**: 4.25 hours
- **Phase 4 (Testing)**: 16 hours
- **Phase 5 (Documentation)**: 7 hours

**Total**: ~46 hours (~6 days)

### Success Criteria

After implementing this plan, the system will have:

✅ All critical bugs fixed
✅ Basic security (TLS + API keys)
✅ Request size limits and timeouts
✅ Proper error handling
✅ S3 pagination working
✅ Metrics properly instrumented
✅ Health checks testing dependencies
✅ Configuration validation
✅ Unit test coverage >70%
✅ Integration tests
✅ Updated documentation

### Out of Scope (Future Work)

- Advanced auth (OAuth2, JWT)
- Circuit breakers
- Advanced rate limiting (distributed)
- Multi-region S3
- Payload compression
- Helm charts

---

## Implementation Order

**Day 1**:
1. Fix metrics endpoint registration (5min)
2. Add metrics instrumentation (2h)
3. Fix S3 pagination (1h)
4. Add request size limits (1h)
5. Fix workflow metadata extraction (3h)
6. Fix archive check logic (1h)

**Day 2**:
7. Add TLS support (3h)
8. Add basic authentication (2h)
9. Add rate limiting (2h)
10. Add input validation (1h)

**Day 3**:
11. Add HTTP/gRPC timeouts (30min)
12. Add S3 operation timeouts (30min)
13. Implement proper health checks (1h)
14. Add configuration validation (1h)
15. Add graceful shutdown timeout (15min)

**Day 4-5**:
16. Write unit tests (8h)
17. Write integration tests (4h)
18. Add distributed tracing (4h)

**Day 6**:
19. Update documentation (2h)
20. Update K8s manifests (2h)
21. Create runbooks (3h)

---

## Testing Plan

After each phase:
1. Run unit tests
2. Start local environment with Docker Compose
3. Run integration tests
4. Verify metrics endpoint
5. Test with both Go and TS workers
6. Load test with k6

Final validation:
- Deploy to staging K8s environment
- Run full test suite
- Performance test
- Security scan
- Load test for 1 hour

---

## Rollback Plan

If issues are found:
1. Each fix is in a separate commit
2. Can rollback individual commits
3. Feature flags for new functionality
4. Staged rollout (canary → 50% → 100%)
