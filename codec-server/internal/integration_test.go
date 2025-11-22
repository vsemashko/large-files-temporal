package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	grpcserver "github.com/vsemashko/large-files-temporal/codec-server/internal/grpc"
	httpserver "github.com/vsemashko/large-files-temporal/codec-server/internal/http"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	pb "github.com/vsemashko/large-files-temporal/codec-server/pkg/proto"
	"go.temporal.io/api/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

const bufSize = 1024 * 1024

// Mock storage for integration tests
type mockStorage struct {
	uploads  map[string][]byte
	metadata map[string]map[string]string
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		uploads:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
}

func (m *mockStorage) Upload(ctx context.Context, key string, data []byte, metadata map[string]string) error {
	m.uploads[key] = data
	m.metadata[key] = metadata
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) ([]byte, error) {
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
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
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
		if prefix == "" || (len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// TestHTTPIntegration tests the full HTTP server integration
func TestHTTPIntegration_SmallPayload(t *testing.T) {
	// Setup
	mock := newMockStorage()
	c := codec.NewCodec(mock, 2*1024*1024) // 2MB threshold
	server := httpserver.NewServer(c, "")

	// Create HTTP test server
	mux := http.NewServeMux()
	mux.HandleFunc("/encode", server.HandleEncode)
	mux.HandleFunc("/decode", server.HandleDecode)
	mux.HandleFunc("/health", server.HandleHealth)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test encode
	encodeReq := httpserver.EncodeRequest{
		Payloads: []httpserver.PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "json/plain"},
				Data:     "small test payload",
			},
		},
		Namespace:  "test-namespace",
		WorkflowID: "test-workflow-123",
		RunID:      "test-run-456",
	}

	body, _ := json.Marshal(encodeReq)
	resp, err := http.Post(ts.URL+"/encode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Encode request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var encodeResp httpserver.EncodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&encodeResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Small payload should remain inline (not in S3)
	if len(mock.uploads) != 0 {
		t.Error("Small payload should not be uploaded to S3")
	}

	// Test decode
	decodeReq := httpserver.DecodeRequest{
		Payloads: encodeResp.Payloads,
	}

	body, _ = json.Marshal(decodeReq)
	resp, err = http.Post(ts.URL+"/decode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Decode request failed: %v", err)
	}
	defer resp.Body.Close()

	var decodeResp httpserver.DecodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decodeResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if decodeResp.Payloads[0].Data != "small test payload" {
		t.Errorf("Decoded data mismatch: got %s", decodeResp.Payloads[0].Data)
	}
}

// TestHTTPIntegration_LargePayload tests large payload handling
func TestHTTPIntegration_LargePayload(t *testing.T) {
	// Setup
	mock := newMockStorage()
	c := codec.NewCodec(mock, 1024) // 1KB threshold
	server := httpserver.NewServer(c, "")

	// Create HTTP test server
	mux := http.NewServeMux()
	mux.HandleFunc("/encode", server.HandleEncode)
	mux.HandleFunc("/decode", server.HandleDecode)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create large payload
	largeData := make([]byte, 2048)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	encodeReq := httpserver.EncodeRequest{
		Payloads: []httpserver.PayloadJSON{
			{
				Metadata: map[string]string{"encoding": "binary/octet-stream"},
				Data:     string(largeData),
			},
		},
		Namespace:  "test-namespace",
		WorkflowID: "test-workflow-large",
		RunID:      "test-run-large",
	}

	// Test encode
	body, _ := json.Marshal(encodeReq)
	resp, err := http.Post(ts.URL+"/encode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Encode request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var encodeResp httpserver.EncodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&encodeResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Large payload should be in S3
	if len(mock.uploads) != 1 {
		t.Errorf("Expected 1 S3 upload for large payload, got %d", len(mock.uploads))
	}

	// Verify S3 key format
	var foundKey string
	for key := range mock.uploads {
		foundKey = key
		// Key should be: namespace/workflowID/runID/hash
		if len(key) < 10 {
			t.Errorf("S3 key too short: %s", key)
		}
	}

	// Test decode
	decodeReq := httpserver.DecodeRequest{
		Payloads: encodeResp.Payloads,
	}

	body, _ = json.Marshal(decodeReq)
	resp, err = http.Post(ts.URL+"/decode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Decode request failed: %v", err)
	}
	defer resp.Body.Close()

	var decodeResp httpserver.DecodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decodeResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify data integrity
	if len(decodeResp.Payloads[0].Data) != len(largeData) {
		t.Errorf("Decoded data length mismatch: got %d, want %d",
			len(decodeResp.Payloads[0].Data), len(largeData))
	}

	// Verify S3 metadata
	metadata := mock.metadata[foundKey]
	if metadata["namespace"] != "test-namespace" {
		t.Errorf("S3 metadata namespace mismatch: got %s", metadata["namespace"])
	}
	if metadata["workflow-id"] != "test-workflow-large" {
		t.Errorf("S3 metadata workflow-id mismatch: got %s", metadata["workflow-id"])
	}
}

// TestHTTPIntegration_Authentication tests API key authentication
func TestHTTPIntegration_Authentication(t *testing.T) {
	// Setup with API key
	mock := newMockStorage()
	c := codec.NewCodec(mock, 2*1024*1024)
	server := httpserver.NewServer(c, "secret-api-key")

	// Create HTTP test server with auth middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/encode", server.AuthMiddleware(server.HandleEncode))
	mux.HandleFunc("/health", server.AuthMiddleware(server.HandleHealth))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	encodeReq := httpserver.EncodeRequest{
		Payloads: []httpserver.PayloadJSON{
			{Data: "test"},
		},
		Namespace:  "test",
		WorkflowID: "test",
		RunID:      "test",
	}

	// Test without API key - should fail
	body, _ := json.Marshal(encodeReq)
	resp, err := http.Post(ts.URL+"/encode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 without API key, got %d", resp.StatusCode)
	}

	// Test with valid API key - should succeed
	req, _ := http.NewRequest("POST", ts.URL+"/encode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret-api-key")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 with valid API key, got %d", resp.StatusCode)
	}

	// Test health endpoint - should not require auth
	resp, err = http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("Health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected health endpoint to not require auth, got status %d", resp.StatusCode)
	}
}

// TestGRPCIntegration tests the full gRPC server integration
func TestGRPCIntegration_SmallPayload(t *testing.T) {
	// Setup
	mock := newMockStorage()
	c := codec.NewCodec(mock, 2*1024*1024) // 2MB threshold
	server := grpcserver.NewServer(c)

	// Create in-memory gRPC server
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	pb.RegisterPayloadCodecServer(s, server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Errorf("Server exited with error: %v", err)
		}
	}()
	defer s.Stop()

	// Create client
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (interface{ Read([]byte) (int, error); Write([]byte) (int, error); Close() error }, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewPayloadCodecClient(conn)

	// Create test payload
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte("small grpc test"),
	}

	payloadBytes, _ := proto.Marshal(testPayload)

	// Test encode
	encodeReq := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "grpc-namespace",
		WorkflowId: "grpc-workflow",
		RunId:      "grpc-run",
	}

	encodeResp, err := client.Encode(ctx, encodeReq)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Small payload should not be in S3
	if len(mock.uploads) != 0 {
		t.Error("Small payload should not be uploaded to S3")
	}

	// Test decode
	decodeReq := &pb.DecodeRequest{
		Payloads: encodeResp.EncodedPayloads,
	}

	decodeResp, err := client.Decode(ctx, decodeReq)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	var decoded common.Payload
	if err := proto.Unmarshal(decodeResp.DecodedPayloads[0], &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if string(decoded.Data) != "small grpc test" {
		t.Errorf("Data mismatch: got %s", string(decoded.Data))
	}
}

// TestGRPCIntegration_LargePayload tests large payload handling via gRPC
func TestGRPCIntegration_LargePayload(t *testing.T) {
	// Setup
	mock := newMockStorage()
	c := codec.NewCodec(mock, 1024) // 1KB threshold
	server := grpcserver.NewServer(c)

	// Create in-memory gRPC server
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	pb.RegisterPayloadCodecServer(s, server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Errorf("Server exited with error: %v", err)
		}
	}()
	defer s.Stop()

	// Create client
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (interface{ Read([]byte) (int, error); Write([]byte) (int, error); Close() error }, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewPayloadCodecClient(conn)

	// Create large payload
	largeData := make([]byte, 4096) // 4KB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("binary/octet-stream"),
		},
		Data: largeData,
	}

	payloadBytes, _ := proto.Marshal(testPayload)

	// Test encode
	encodeReq := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "grpc-ns",
		WorkflowId: "grpc-wf-large",
		RunId:      "grpc-run-large",
	}

	encodeResp, err := client.Encode(ctx, encodeReq)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Large payload should be in S3
	if len(mock.uploads) != 1 {
		t.Errorf("Expected 1 S3 upload, got %d", len(mock.uploads))
	}

	// Test decode
	decodeReq := &pb.DecodeRequest{
		Payloads: encodeResp.EncodedPayloads,
	}

	decodeResp, err := client.Decode(ctx, decodeReq)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	var decoded common.Payload
	if err := proto.Unmarshal(decodeResp.DecodedPayloads[0], &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify data integrity
	if !bytes.Equal(decoded.Data, largeData) {
		t.Error("Large payload data mismatch after roundtrip")
	}
}

// TestGRPCIntegration_Authentication tests gRPC API key authentication
func TestGRPCIntegration_Authentication(t *testing.T) {
	// Setup with API key
	mock := newMockStorage()
	c := codec.NewCodec(mock, 2*1024*1024)
	server := grpcserver.NewServer(c)

	// Create in-memory gRPC server with auth interceptor
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer(
		grpc.UnaryInterceptor(grpcserver.AuthInterceptor("secret-grpc-key")),
	)
	pb.RegisterPayloadCodecServer(s, server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Errorf("Server exited with error: %v", err)
		}
	}()
	defer s.Stop()

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (interface{ Read([]byte) (int, error); Write([]byte) (int, error); Close() error }, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewPayloadCodecClient(conn)

	testPayload := &common.Payload{
		Data: []byte("test"),
	}
	payloadBytes, _ := proto.Marshal(testPayload)

	encodeReq := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test",
		WorkflowId: "test",
		RunId:      "test",
	}

	// Test without API key - should fail
	_, err = client.Encode(ctx, encodeReq)
	if err == nil {
		t.Error("Expected error without API key")
	}

	// Test with valid API key - should succeed
	md := metadata.New(map[string]string{
		"x-api-key": "secret-grpc-key",
	})
	ctxWithAuth := metadata.NewOutgoingContext(ctx, md)

	_, err = client.Encode(ctxWithAuth, encodeReq)
	if err != nil {
		t.Errorf("Expected success with valid API key, got: %v", err)
	}
}

// TestIntegration_ConcurrentRequests tests concurrent encode/decode operations
func TestIntegration_ConcurrentRequests(t *testing.T) {
	// Setup
	mock := newMockStorage()
	c := codec.NewCodec(mock, 1024)
	server := httpserver.NewServer(c, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/encode", server.HandleEncode)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Run concurrent requests
	numRequests := 10
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			encodeReq := httpserver.EncodeRequest{
				Payloads: []httpserver.PayloadJSON{
					{Data: fmt.Sprintf("concurrent test %d", id)},
				},
				Namespace:  "test",
				WorkflowID: fmt.Sprintf("workflow-%d", id),
				RunID:      fmt.Sprintf("run-%d", id),
			}

			body, _ := json.Marshal(encodeReq)
			resp, err := http.Post(ts.URL+"/encode", "application/json", bytes.NewReader(body))
			if err != nil {
				errors <- err
				done <- false
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("request %d failed with status %d", id, resp.StatusCode)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all requests with timeout
	timeout := time.After(5 * time.Second)
	successCount := 0

	for i := 0; i < numRequests; i++ {
		select {
		case success := <-done:
			if success {
				successCount++
			}
		case err := <-errors:
			t.Errorf("Concurrent request error: %v", err)
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent requests")
		}
	}

	if successCount != numRequests {
		t.Errorf("Expected %d successful requests, got %d", numRequests, successCount)
	}
}

// TestIntegration_MetadataPreservation tests that metadata is preserved through encode/decode
func TestIntegration_MetadataPreservation(t *testing.T) {
	mock := newMockStorage()
	c := codec.NewCodec(mock, 1024)
	server := httpserver.NewServer(c, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/encode", server.HandleEncode)
	mux.HandleFunc("/decode", server.HandleDecode)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test with rich metadata
	encodeReq := httpserver.EncodeRequest{
		Payloads: []httpserver.PayloadJSON{
			{
				Metadata: map[string]string{
					"encoding":     "json/plain",
					"content-type": "application/json",
					"custom-field": "custom-value",
				},
				Data: make([]byte, 2048), // Large enough to go to S3
			},
		},
		Namespace:  "metadata-test",
		WorkflowID: "metadata-workflow",
		RunID:      "metadata-run",
	}

	// Encode
	body, _ := json.Marshal(encodeReq)
	resp, err := http.Post(ts.URL+"/encode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	defer resp.Body.Close()

	var encodeResp httpserver.EncodeResponse
	json.NewDecoder(resp.Body).Decode(&encodeResp)

	// Decode
	decodeReq := httpserver.DecodeRequest{
		Payloads: encodeResp.Payloads,
	}

	body, _ = json.Marshal(decodeReq)
	resp, err = http.Post(ts.URL+"/decode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	defer resp.Body.Close()

	var decodeResp httpserver.DecodeResponse
	json.NewDecoder(resp.Body).Decode(&decodeResp)

	// Verify metadata preservation
	originalMetadata := encodeReq.Payloads[0].Metadata
	decodedMetadata := decodeResp.Payloads[0].Metadata

	for key, value := range originalMetadata {
		if decodedMetadata[key] != value {
			t.Errorf("Metadata mismatch for key %s: got %s, want %s",
				key, decodedMetadata[key], value)
		}
	}
}
