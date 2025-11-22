package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	pb "github.com/vsemashko/large-files-temporal/codec-server/pkg/proto"
	"go.temporal.io/api/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
	return NewServer(c)
}

func TestEncode_Success(t *testing.T) {
	server := newTestServer()

	// Create test payload
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte("test data"),
	}

	payloadBytes, err := proto.Marshal(testPayload)
	if err != nil {
		t.Fatalf("Failed to marshal test payload: %v", err)
	}

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	resp, err := server.Encode(context.Background(), req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(resp.EncodedPayloads) != 1 {
		t.Errorf("Expected 1 encoded payload, got %d", len(resp.EncodedPayloads))
	}

	// Verify we can unmarshal the response
	var encoded common.Payload
	if err := proto.Unmarshal(resp.EncodedPayloads[0], &encoded); err != nil {
		t.Errorf("Failed to unmarshal encoded payload: %v", err)
	}
}

func TestEncode_MultiplePayloads(t *testing.T) {
	server := newTestServer()

	payloads := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		testPayload := &common.Payload{
			Metadata: map[string][]byte{
				"encoding": []byte("json/plain"),
			},
			Data: []byte("test data"),
		}

		payloadBytes, err := proto.Marshal(testPayload)
		if err != nil {
			t.Fatalf("Failed to marshal test payload %d: %v", i, err)
		}
		payloads[i] = payloadBytes
	}

	req := &pb.EncodeRequest{
		Payloads:   payloads,
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	resp, err := server.Encode(context.Background(), req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(resp.EncodedPayloads) != 3 {
		t.Errorf("Expected 3 encoded payloads, got %d", len(resp.EncodedPayloads))
	}
}

func TestEncode_InvalidPayload(t *testing.T) {
	server := newTestServer()

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{[]byte("invalid proto data")},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	_, err := server.Encode(context.Background(), req)
	if err == nil {
		t.Error("Expected error for invalid payload")
	}
}

func TestDecode_Success(t *testing.T) {
	server := newTestServer()

	// Create test payload
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte("test data"),
	}

	payloadBytes, err := proto.Marshal(testPayload)
	if err != nil {
		t.Fatalf("Failed to marshal test payload: %v", err)
	}

	req := &pb.DecodeRequest{
		Payloads: [][]byte{payloadBytes},
	}

	resp, err := server.Decode(context.Background(), req)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(resp.DecodedPayloads) != 1 {
		t.Errorf("Expected 1 decoded payload, got %d", len(resp.DecodedPayloads))
	}

	// Verify we can unmarshal the response
	var decoded common.Payload
	if err := proto.Unmarshal(resp.DecodedPayloads[0], &decoded); err != nil {
		t.Errorf("Failed to unmarshal decoded payload: %v", err)
	}
}

func TestDecode_InvalidPayload(t *testing.T) {
	server := newTestServer()

	req := &pb.DecodeRequest{
		Payloads: [][]byte{[]byte("invalid proto data")},
	}

	_, err := server.Decode(context.Background(), req)
	if err == nil {
		t.Error("Expected error for invalid payload")
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	server := newTestServer()

	// Create test payload
	originalPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
			"custom":   []byte("metadata"),
		},
		Data: []byte("test data with special chars: 日本語"),
	}

	payloadBytes, err := proto.Marshal(originalPayload)
	if err != nil {
		t.Fatalf("Failed to marshal test payload: %v", err)
	}

	// Encode
	encodeReq := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	encodeResp, err := server.Encode(context.Background(), encodeReq)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decodeReq := &pb.DecodeRequest{
		Payloads: encodeResp.EncodedPayloads,
	}

	decodeResp, err := server.Decode(context.Background(), decodeReq)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify roundtrip
	var resultPayload common.Payload
	if err := proto.Unmarshal(decodeResp.DecodedPayloads[0], &resultPayload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if string(resultPayload.Data) != string(originalPayload.Data) {
		t.Errorf("Data mismatch after roundtrip:\nOriginal: %s\nResult:   %s",
			string(originalPayload.Data), string(resultPayload.Data))
	}

	if string(resultPayload.Metadata["encoding"]) != string(originalPayload.Metadata["encoding"]) {
		t.Error("Metadata mismatch after roundtrip")
	}
}

func TestAuthInterceptor_NoAPIKey(t *testing.T) {
	interceptor := AuthInterceptor("")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Errorf("Expected no error when API key not configured, got: %v", err)
	}

	if resp != "success" {
		t.Errorf("Expected handler to be called")
	}
}

func TestAuthInterceptor_ValidAPIKeyInXAPIKey(t *testing.T) {
	interceptor := AuthInterceptor("test-api-key-12345")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	// Create context with API key in metadata
	md := metadata.New(map[string]string{
		"x-api-key": "test-api-key-12345",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Errorf("Expected no error with valid API key, got: %v", err)
	}

	if resp != "success" {
		t.Errorf("Expected handler to be called")
	}
}

func TestAuthInterceptor_ValidAPIKeyInBearerToken(t *testing.T) {
	interceptor := AuthInterceptor("test-api-key-12345")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	// Create context with API key in Authorization header
	md := metadata.New(map[string]string{
		"authorization": "Bearer test-api-key-12345",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Errorf("Expected no error with valid Bearer token, got: %v", err)
	}

	if resp != "success" {
		t.Errorf("Expected handler to be called")
	}
}

func TestAuthInterceptor_InvalidAPIKey(t *testing.T) {
	interceptor := AuthInterceptor("test-api-key-12345")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	// Create context with wrong API key
	md := metadata.New(map[string]string{
		"x-api-key": "wrong-api-key",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, err := interceptor(ctx, nil, info, handler)
	if err == nil {
		t.Error("Expected error with invalid API key")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Error("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestAuthInterceptor_MissingAPIKey(t *testing.T) {
	interceptor := AuthInterceptor("test-api-key-12345")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	// Create context with no API key
	md := metadata.New(map[string]string{})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, err := interceptor(ctx, nil, info, handler)
	if err == nil {
		t.Error("Expected error when API key is missing")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Error("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	interceptor := AuthInterceptor("test-api-key-12345")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	// Context without metadata
	_, err := interceptor(context.Background(), nil, info, handler)
	if err == nil {
		t.Error("Expected error when metadata is missing")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Error("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestEncode_LargePayload(t *testing.T) {
	server := newTestServer()

	// Create large payload that will be stored to S3
	largeData := make([]byte, 2048) // 2KB, exceeds 1KB threshold
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("binary/octet-stream"),
		},
		Data: largeData,
	}

	payloadBytes, err := proto.Marshal(testPayload)
	if err != nil {
		t.Fatalf("Failed to marshal test payload: %v", err)
	}

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	resp, err := server.Encode(context.Background(), req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify response
	if len(resp.EncodedPayloads) != 1 {
		t.Errorf("Expected 1 encoded payload, got %d", len(resp.EncodedPayloads))
	}

	// Decode to verify roundtrip
	decodeReq := &pb.DecodeRequest{
		Payloads: resp.EncodedPayloads,
	}

	decodeResp, err := server.Decode(context.Background(), decodeReq)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	var decoded common.Payload
	if err := proto.Unmarshal(decodeResp.DecodedPayloads[0], &decoded); err != nil {
		t.Fatalf("Failed to unmarshal decoded payload: %v", err)
	}

	// Verify data integrity
	if len(decoded.Data) != len(largeData) {
		t.Errorf("Data length mismatch: got %d, want %d", len(decoded.Data), len(largeData))
	}

	for i := range largeData {
		if decoded.Data[i] != largeData[i] {
			t.Errorf("Data mismatch at byte %d: got %d, want %d", i, decoded.Data[i], largeData[i])
			break
		}
	}
}

func TestEncode_EmptyPayload(t *testing.T) {
	server := newTestServer()

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	resp, err := server.Encode(context.Background(), req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(resp.EncodedPayloads) != 0 {
		t.Errorf("Expected 0 encoded payloads, got %d", len(resp.EncodedPayloads))
	}
}

func BenchmarkEncode_SmallPayload(b *testing.B) {
	server := newTestServer()

	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte("test data"),
	}

	payloadBytes, _ := proto.Marshal(testPayload)

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.Encode(context.Background(), req)
	}
}

func BenchmarkEncode_LargePayload(b *testing.B) {
	server := newTestServer()

	largeData := make([]byte, 1024*1024) // 1MB
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("binary/octet-stream"),
		},
		Data: largeData,
	}

	payloadBytes, _ := proto.Marshal(testPayload)

	req := &pb.EncodeRequest{
		Payloads:   [][]byte{payloadBytes},
		Namespace:  "test-namespace",
		WorkflowId: "test-workflow",
		RunId:      "test-run",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.Encode(context.Background(), req)
	}
}

func BenchmarkDecode(b *testing.B) {
	server := newTestServer()

	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte("test data"),
	}

	payloadBytes, _ := proto.Marshal(testPayload)

	req := &pb.DecodeRequest{
		Payloads: [][]byte{payloadBytes},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.Decode(context.Background(), req)
	}
}
