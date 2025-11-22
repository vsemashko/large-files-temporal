package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	"go.temporal.io/api/common/v1"
)

// Mock storage for testing
type mockStorage struct {
	uploads   map[string][]byte
	metadata  map[string]map[string]string
	uploadErr error
	downloadErr error
}

func (m *mockStorage) Upload(ctx context.Context, key string, data []byte, metadata map[string]string) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	m.uploads[key] = data
	m.metadata[key] = metadata
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) ([]byte, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	data, ok := m.uploads[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return data, nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	delete(m.uploads, key)
	delete(m.metadata, key)
	return nil
}

func (m *mockStorage) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	count := 0
	for key := range m.uploads {
		if strings.HasPrefix(key, prefix) {
			delete(m.uploads, key)
			delete(m.metadata, key)
			count++
		}
	}
	return count, nil
}

func (m *mockStorage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for key := range m.uploads {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func newTestServer() *Server {
	mock := &mockStorage{
		uploads:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
	c := codec.NewCodec(mock, 1024) // 1KB threshold
	return NewServer(c, "")
}

func newTestServerWithAuth() *Server {
	mock := &mockStorage{
		uploads:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
	c := codec.NewCodec(mock, 1024)
	return NewServer(c, "test-api-key-12345")
}

func TestAuthMiddleware_NoAPIKey(t *testing.T) {
	server := newTestServer()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_HealthEndpointNoAuth(t *testing.T) {
	server := newTestServerWithAuth()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health endpoint should not require auth, got status %d", w.Code)
	}
}

func TestAuthMiddleware_ValidAPIKeyInXAPIKeyHeader(t *testing.T) {
	server := newTestServerWithAuth()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("POST", "/encode", nil)
	req.Header.Set("X-API-Key", "test-api-key-12345")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 with valid API key, got %d", w.Code)
	}
}

func TestAuthMiddleware_ValidAPIKeyInBearerToken(t *testing.T) {
	server := newTestServerWithAuth()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("POST", "/encode", nil)
	req.Header.Set("Authorization", "Bearer test-api-key-12345")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 with valid Bearer token, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidAPIKey(t *testing.T) {
	server := newTestServerWithAuth()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("POST", "/encode", nil)
	req.Header.Set("X-API-Key", "wrong-api-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 with invalid API key, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingAPIKey(t *testing.T) {
	server := newTestServerWithAuth()

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("POST", "/encode", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 with missing API key, got %d", w.Code)
	}
}

func TestHandleEncode_Success(t *testing.T) {
	server := newTestServer()

	reqBody := EncodeRequest{
		Payloads: []PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "json/plain"},
				Data:     "test data",
			},
		},
		Namespace:  "test-namespace",
		WorkflowID: "test-workflow",
		RunID:      "test-run",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/encode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp EncodeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Payloads) != 1 {
		t.Errorf("Expected 1 payload in response, got %d", len(resp.Payloads))
	}
}

func TestHandleEncode_MethodNotAllowed(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest("GET", "/encode", nil)
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleEncode_InvalidJSON(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest("POST", "/encode", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandleEncode_MissingNamespace(t *testing.T) {
	server := newTestServer()

	reqBody := EncodeRequest{
		Payloads: []PayloadJSON{
			{Data: "test"},
		},
		// Namespace missing
		WorkflowID: "test-workflow",
		RunID:      "test-run",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/encode", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing namespace, got %d", w.Code)
	}
}

func TestHandleEncode_TooManyPayloads(t *testing.T) {
	server := newTestServer()

	payloads := make([]PayloadJSON, MaxPayloadsPerRequest+1)
	for i := range payloads {
		payloads[i] = PayloadJSON{Data: "test"}
	}

	reqBody := EncodeRequest{
		Payloads:   payloads,
		Namespace:  "test",
		WorkflowID: "test",
		RunID:      "test",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/encode", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for too many payloads, got %d", w.Code)
	}
}

func TestHandleDecode_Success(t *testing.T) {
	server := newTestServer()

	reqBody := DecodeRequest{
		Payloads: []PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "json/plain"},
				Data:     "test data",
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/decode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleDecode(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DecodeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Payloads) != 1 {
		t.Errorf("Expected 1 payload in response, got %d", len(resp.Payloads))
	}
}

func TestHandleDecode_MethodNotAllowed(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest("GET", "/decode", nil)
	w := httptest.NewRecorder()

	server.HandleDecode(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleHealth_Success(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", resp.Status)
	}

	if resp.Checks["s3"] != "ok" {
		t.Errorf("Expected s3 check 'ok', got '%s'", resp.Checks["s3"])
	}

	if resp.Checks["codec"] != "ok" {
		t.Errorf("Expected codec check 'ok', got '%s'", resp.Checks["codec"])
	}
}

func TestValidateEncodeRequest_Valid(t *testing.T) {
	req := &EncodeRequest{
		Payloads: []PayloadJSON{
			{Data: "test"},
		},
		Namespace:  "test-namespace",
		WorkflowID: "test-workflow",
		RunID:      "test-run",
	}

	if err := validateEncodeRequest(req); err != nil {
		t.Errorf("Valid request should not error: %v", err)
	}
}

func TestValidateEncodeRequest_MissingNamespace(t *testing.T) {
	req := &EncodeRequest{
		Payloads:   []PayloadJSON{{Data: "test"}},
		WorkflowID: "test",
		RunID:      "test",
	}

	err := validateEncodeRequest(req)
	if err == nil {
		t.Error("Expected error for missing namespace")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("Expected error about namespace, got: %v", err)
	}
}

func TestValidateEncodeRequest_MissingWorkflowID(t *testing.T) {
	req := &EncodeRequest{
		Payloads:  []PayloadJSON{{Data: "test"}},
		Namespace: "test",
		RunID:     "test",
	}

	err := validateEncodeRequest(req)
	if err == nil {
		t.Error("Expected error for missing workflowId")
	}
	if !strings.Contains(err.Error(), "workflowId") {
		t.Errorf("Expected error about workflowId, got: %v", err)
	}
}

func TestValidateEncodeRequest_TooManyPayloads(t *testing.T) {
	payloads := make([]PayloadJSON, MaxPayloadsPerRequest+1)
	req := &EncodeRequest{
		Payloads:   payloads,
		Namespace:  "test",
		WorkflowID: "test",
		RunID:      "test",
	}

	err := validateEncodeRequest(req)
	if err == nil {
		t.Error("Expected error for too many payloads")
	}
	if !strings.Contains(err.Error(), "too many payloads") {
		t.Errorf("Expected error about too many payloads, got: %v", err)
	}
}

func TestValidatePayload_Valid(t *testing.T) {
	payload := PayloadJSON{
		Metadata: map[string]string{
			"key": "value",
		},
		Data: "test data",
	}

	if err := validatePayload(payload, 0); err != nil {
		t.Errorf("Valid payload should not error: %v", err)
	}
}

func TestValidatePayload_TooManyMetadataEntries(t *testing.T) {
	metadata := make(map[string]string)
	for i := 0; i < 101; i++ {
		metadata[string(rune(i))] = "value"
	}

	payload := PayloadJSON{
		Metadata: metadata,
		Data:     "test",
	}

	err := validatePayload(payload, 0)
	if err == nil {
		t.Error("Expected error for too many metadata entries")
	}
	if !strings.Contains(err.Error(), "too many metadata") {
		t.Errorf("Expected error about metadata count, got: %v", err)
	}
}

func TestValidatePayload_MetadataKeyTooLong(t *testing.T) {
	longKey := strings.Repeat("k", MaxMetadataKeySize+1)
	payload := PayloadJSON{
		Metadata: map[string]string{
			longKey: "value",
		},
		Data: "test",
	}

	err := validatePayload(payload, 0)
	if err == nil {
		t.Error("Expected error for metadata key too long")
	}
	if !strings.Contains(err.Error(), "metadata key too long") {
		t.Errorf("Expected error about key length, got: %v", err)
	}
}

func TestValidatePayload_MetadataValueTooLong(t *testing.T) {
	longValue := strings.Repeat("v", MaxMetadataValueSize+1)
	payload := PayloadJSON{
		Metadata: map[string]string{
			"key": longValue,
		},
		Data: "test",
	}

	err := validatePayload(payload, 0)
	if err == nil {
		t.Error("Expected error for metadata value too long")
	}
	if !strings.Contains(err.Error(), "metadata value too long") {
		t.Errorf("Expected error about value length, got: %v", err)
	}
}

func TestJSONToPayloads(t *testing.T) {
	jsonPayloads := []PayloadJSON{
		{
			Metadata: map[string]string{
				"encoding": "json/plain",
				"type":     "test",
			},
			Data: "test data",
		},
	}

	payloads, err := jsonToPayloads(jsonPayloads)
	if err != nil {
		t.Fatalf("jsonToPayloads failed: %v", err)
	}

	if len(payloads) != 1 {
		t.Errorf("Expected 1 payload, got %d", len(payloads))
	}

	if string(payloads[0].Metadata["encoding"]) != "json/plain" {
		t.Errorf("Metadata not converted correctly")
	}

	if string(payloads[0].Data) != "test data" {
		t.Errorf("Data not converted correctly")
	}
}

func TestPayloadsToJSON(t *testing.T) {
	payloads := []*common.Payload{
		{
			Metadata: map[string][]byte{
				"encoding": []byte("json/plain"),
				"type":     []byte("test"),
			},
			Data: []byte("test data"),
		},
	}

	jsonPayloads, err := payloadsToJSON(payloads)
	if err != nil {
		t.Fatalf("payloadsToJSON failed: %v", err)
	}

	if len(jsonPayloads) != 1 {
		t.Errorf("Expected 1 payload, got %d", len(jsonPayloads))
	}

	if jsonPayloads[0].Metadata["encoding"] != "json/plain" {
		t.Errorf("Metadata not converted correctly")
	}

	if jsonPayloads[0].Data != "test data" {
		t.Errorf("Data not converted correctly")
	}
}

func TestRoundtripConversion(t *testing.T) {
	original := []PayloadJSON{
		{
			Metadata: map[string]string{
				"encoding": "json/plain",
				"custom":   "metadata",
			},
			Data: "test data with special chars: 日本語",
		},
	}

	// JSON -> Temporal
	temporalPayloads, err := jsonToPayloads(original)
	if err != nil {
		t.Fatalf("jsonToPayloads failed: %v", err)
	}

	// Temporal -> JSON
	result, err := payloadsToJSON(temporalPayloads)
	if err != nil {
		t.Fatalf("payloadsToJSON failed: %v", err)
	}

	// Verify roundtrip
	if len(result) != len(original) {
		t.Errorf("Length mismatch after roundtrip")
	}

	if result[0].Data != original[0].Data {
		t.Errorf("Data mismatch after roundtrip")
	}

	for key, value := range original[0].Metadata {
		if result[0].Metadata[key] != value {
			t.Errorf("Metadata mismatch for key %s", key)
		}
	}
}

func TestHandleEncode_LargeRequestBody(t *testing.T) {
	server := newTestServer()

	// Create request larger than max size
	largeData := strings.Repeat("x", MaxRequestBodySize+1)
	req := httptest.NewRequest("POST", "/encode", strings.NewReader(largeData))
	w := httptest.NewRecorder()

	server.HandleEncode(w, req)

	// Should fail due to size limit
	if w.Code == http.StatusOK {
		t.Error("Expected error for request body too large")
	}
}

func BenchmarkHandleEncode(b *testing.B) {
	server := newTestServer()

	reqBody := EncodeRequest{
		Payloads: []PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "json/plain"},
				Data:     "test data",
			},
		},
		Namespace:  "test-namespace",
		WorkflowID: "test-workflow",
		RunID:      "test-run",
	}

	body, _ := json.Marshal(reqBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/encode", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.HandleEncode(w, req)
	}
}

func BenchmarkHandleDecode(b *testing.B) {
	server := newTestServer()

	reqBody := DecodeRequest{
		Payloads: []PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "json/plain"},
				Data:     "test data",
			},
		},
	}

	body, _ := json.Marshal(reqBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/decode", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.HandleDecode(w, req)
	}
}
