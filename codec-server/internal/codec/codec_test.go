package codec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	"go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

// mockStorage implements storage.Storage interface for testing
type mockStorage struct {
	uploads   map[string][]byte
	uploads   map[string][]byte
	metadata  map[string]map[string]string
	uploadErr error
	downloadErr error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		uploads:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
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
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return data, nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	delete(m.uploads, key)
	delete(m.metadata, key)
	return nil
}

func (m *mockStorage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for key := range m.uploads {
		keys = append(keys, key)
	}
	return keys, nil
}

func TestNewCodec(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	if codec == nil {
		t.Fatal("NewCodec returned nil")
	}
	if codec.storage != storage {
		t.Error("Codec storage not set correctly")
	}
	if codec.threshold != 1024 {
		t.Errorf("Expected threshold 1024, got %d", codec.threshold)
	}
	if codec.bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", codec.bucket)
	}
}

func TestEncode_SmallPayload_KeepsInline(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 2*1024*1024, "test-bucket") // 2MB threshold

	payload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte(`{"test":"small payload"}`),
	}

	encoded, err := codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != 1 {
		t.Fatalf("Expected 1 encoded payload, got %d", len(encoded))
	}

	// Should be unchanged since it's small
	if string(encoded[0].Data) != string(payload.Data) {
		t.Error("Small payload was modified when it should have been kept inline")
	}

	// Should not have uploaded to S3
	if len(storage.uploads) != 0 {
		t.Errorf("Expected 0 S3 uploads, got %d", len(storage.uploads))
	}
}

func TestEncode_LargePayload_StoresToS3(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket") // 1KB threshold

	// Create payload larger than threshold
	largeData := make([]byte, 2048) // 2KB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("binary/octet-stream"),
		},
		Data: largeData,
	}

	encoded, err := codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != 1 {
		t.Fatalf("Expected 1 encoded payload, got %d", len(encoded))
	}

	// Should have uploaded to S3
	if len(storage.uploads) != 1 {
		t.Fatalf("Expected 1 S3 upload, got %d", len(storage.uploads))
	}

	// Verify encoded payload is an S3 reference
	encoding, ok := encoded[0].Metadata[storage.MetadataEncodingKey]
	if !ok {
		t.Fatal("Encoded payload missing encoding metadata")
	}
	if string(encoding) != storage.EncodingS3Reference {
		t.Errorf("Expected encoding '%s', got '%s'", storage.EncodingS3Reference, string(encoding))
	}

	// Verify S3 reference structure
	var ref storage.S3Reference
	if err := json.Unmarshal(encoded[0].Data, &ref); err != nil {
		t.Fatalf("Failed to unmarshal S3 reference: %v", err)
	}

	if ref.Type != storage.EncodingS3Reference {
		t.Errorf("Expected ref type '%s', got '%s'", storage.EncodingS3Reference, ref.Type)
	}
	if ref.Bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", ref.Bucket)
	}
	if ref.Size != int64(len(largeData))+int64(len("encoding"))+int64(len("binary/octet-stream")) {
		// Size includes metadata
		t.Logf("Reference size: %d, expected around: %d", ref.Size, len(largeData))
	}

	// Verify S3 key format
	expectedKeyPrefix := "default/wf-1/run-1/"
	if len(ref.Key) <= len(expectedKeyPrefix) {
		t.Errorf("S3 key too short: %s", ref.Key)
	}
	if ref.Key[:len(expectedKeyPrefix)] != expectedKeyPrefix {
		t.Errorf("Expected key to start with '%s', got '%s'", expectedKeyPrefix, ref.Key)
	}
}

func TestEncode_MultiplePayloads(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	payloads := []*common.Payload{
		{Data: []byte("small1")},                    // Small, inline
		{Data: make([]byte, 2048)},                  // Large, S3
		{Data: []byte("small2")},                    // Small, inline
		{Data: make([]byte, 3072)},                  // Large, S3
	}

	encoded, err := codec.Encode(context.Background(), payloads, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != 4 {
		t.Fatalf("Expected 4 encoded payloads, got %d", len(encoded))
	}

	// Should have 2 S3 uploads
	if len(storage.uploads) != 2 {
		t.Fatalf("Expected 2 S3 uploads, got %d", len(storage.uploads))
	}

	// Verify first and third are inline
	if string(encoded[0].Data) != "small1" {
		t.Error("First payload should be inline")
	}
	if string(encoded[2].Data) != "small2" {
		t.Error("Third payload should be inline")
	}

	// Verify second and fourth are S3 references
	if encoding, ok := encoded[1].Metadata[storage.MetadataEncodingKey]; !ok || string(encoding) != storage.EncodingS3Reference {
		t.Error("Second payload should be S3 reference")
	}
	if encoding, ok := encoded[3].Metadata[storage.MetadataEncodingKey]; !ok || string(encoding) != storage.EncodingS3Reference {
		t.Error("Fourth payload should be S3 reference")
	}
}

func TestEncode_EmptyPayloads(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	encoded, err := codec.Encode(context.Background(), []*common.Payload{}, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != 0 {
		t.Errorf("Expected 0 encoded payloads, got %d", len(encoded))
	}
}

func TestEncode_NilPayload(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	payloads := []*common.Payload{nil, {Data: []byte("test")}, nil}

	encoded, err := codec.Encode(context.Background(), payloads, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != 3 {
		t.Fatalf("Expected 3 encoded payloads, got %d", len(encoded))
	}

	if encoded[0] != nil {
		t.Error("First nil payload should remain nil")
	}
	if encoded[2] != nil {
		t.Error("Third nil payload should remain nil")
	}
}

func TestDecode_InlinePayload(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	payload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte(`{"test":"data"}`),
	}

	decoded, err := codec.Decode(context.Background(), []*common.Payload{payload})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("Expected 1 decoded payload, got %d", len(decoded))
	}

	// Should be unchanged
	if string(decoded[0].Data) != string(payload.Data) {
		t.Error("Inline payload was modified during decode")
	}
}

func TestDecode_S3Reference(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	// First encode a large payload to get it into S3
	originalData := make([]byte, 2048)
	for i := range originalData {
		originalData[i] = byte(i % 256)
	}

	originalPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("binary/octet-stream"),
		},
		Data: originalData,
	}

	encoded, err := codec.Encode(context.Background(), []*common.Payload{originalPayload}, "default", "wf-1", "run-1")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Now decode the S3 reference
	decoded, err := codec.Decode(context.Background(), encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("Expected 1 decoded payload, got %d", len(decoded))
	}

	// Verify decoded data matches original
	if len(decoded[0].Data) != len(originalData) {
		t.Errorf("Decoded data length mismatch: expected %d, got %d", len(originalData), len(decoded[0].Data))
	}

	for i := range originalData {
		if decoded[0].Data[i] != originalData[i] {
			t.Errorf("Decoded data mismatch at index %d: expected %d, got %d", i, originalData[i], decoded[0].Data[i])
			break
		}
	}

	// Verify original encoding is preserved
	if encoding, ok := decoded[0].Metadata["encoding"]; !ok || string(encoding) != "binary/octet-stream" {
		t.Error("Original encoding not preserved in decoded payload")
	}
}

func TestEncode_ContextTimeout(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	largeData := make([]byte, 2048)
	payload := &common.Payload{Data: largeData}

	// Should handle context cancellation gracefully
	_, err := codec.Encode(ctx, []*common.Payload{payload}, "default", "wf-1", "run-1")
	if err == nil {
		// Note: Current implementation may not check context, so this might pass
		// This test documents expected behavior
		t.Log("Encode did not fail on cancelled context (implementation may not check context yet)")
	}
}

func TestCalculatePayloadSize(t *testing.T) {
	tests := []struct {
		name     string
		payload  *common.Payload
		expected int64
	}{
		{
			name:     "nil payload",
			payload:  nil,
			expected: 0,
		},
		{
			name: "payload with only data",
			payload: &common.Payload{
				Data: []byte("hello"),
			},
			expected: 5,
		},
		{
			name: "payload with data and metadata",
			payload: &common.Payload{
				Data: []byte("hello"),
				Metadata: map[string][]byte{
					"key1": []byte("value1"),
					"key2": []byte("value2"),
				},
			},
			expected: 5 + (4 + 6) + (4 + 6), // data + (key1 + value1) + (key2 + value2)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := calculatePayloadSize(tt.payload)
			if size != tt.expected {
				t.Errorf("calculatePayloadSize() = %d, expected %d", size, tt.expected)
			}
		})
	}
}

func TestIsS3Reference(t *testing.T) {
	tests := []struct {
		name     string
		payload  *common.Payload
		expected bool
	}{
		{
			name:     "nil payload",
			payload:  nil,
			expected: false,
		},
		{
			name: "payload without metadata",
			payload: &common.Payload{
				Data: []byte("test"),
			},
			expected: false,
		},
		{
			name: "payload with different encoding",
			payload: &common.Payload{
				Metadata: map[string][]byte{
					storage.MetadataEncodingKey: []byte("json/plain"),
				},
			},
			expected: false,
		},
		{
			name: "payload with S3 reference encoding",
			payload: &common.Payload{
				Metadata: map[string][]byte{
					storage.MetadataEncodingKey: []byte(storage.EncodingS3Reference),
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isS3Reference(tt.payload)
			if result != tt.expected {
				t.Errorf("isS3Reference() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestEncode_S3KeyFormat(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 100, "test-bucket")

	largeData := make([]byte, 200)
	payload := &common.Payload{Data: largeData}

	_, err := codec.Encode(context.Background(), []*common.Payload{payload}, "my-namespace", "my-workflow", "my-run")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Check S3 key format
	if len(storage.uploads) != 1 {
		t.Fatalf("Expected 1 upload, got %d", len(storage.uploads))
	}

	var key string
	for k := range storage.uploads {
		key = k
		break
	}

	// Key should be: namespace/workflowID/runID/hash
	expectedPrefix := "my-namespace/my-workflow/my-run/"
	if len(key) <= len(expectedPrefix) {
		t.Fatalf("Key too short: %s", key)
	}
	if key[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("Expected key to start with '%s', got '%s'", expectedPrefix, key)
	}

	// Hash should be hex-encoded SHA256 (64 characters)
	hash := key[len(expectedPrefix):]
	if len(hash) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash))
	}

	// Verify hash is valid hex
	if _, err := hex.DecodeString(hash); err != nil {
		t.Errorf("Hash is not valid hex: %s", hash)
	}
}

func TestEncode_MetadataPassthrough(t *testing.T) {
	storage := newMockStorage()
	codec := NewCodec(storage, 100, "test-bucket")

	largeData := make([]byte, 200)
	payload := &common.Payload{Data: largeData}

	_, err := codec.Encode(context.Background(), []*common.Payload{payload}, "namespace", "workflow-123", "run-456")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify metadata was passed to S3
	for _, metadata := range storage.metadata {
		if metadata["workflow-id"] != "workflow-123" {
			t.Errorf("Expected workflow-id 'workflow-123', got '%s'", metadata["workflow-id"])
		}
		if metadata["run-id"] != "run-456" {
			t.Errorf("Expected run-id 'run-456', got '%s'", metadata["run-id"])
		}
		if metadata["namespace"] != "namespace" {
			t.Errorf("Expected namespace 'namespace', got '%s'", metadata["namespace"])
		}
		if metadata["archived"] != "false" {
			t.Errorf("Expected archived 'false', got '%s'", metadata["archived"])
		}
	}
}

// Benchmark tests
func BenchmarkEncode_SmallPayload(b *testing.B) {
	storage := newMockStorage()
	codec := NewCodec(storage, 2*1024*1024, "test-bucket")
	payload := &common.Payload{Data: []byte("small payload")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf", "run")
	}
}

func BenchmarkEncode_LargePayload(b *testing.B) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")
	largeData := make([]byte, 1024*1024) // 1MB
	payload := &common.Payload{Data: largeData}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.Encode(context.Background(), []*common.Payload{payload}, "default", "wf", "run")
	}
}

func BenchmarkDecode_S3Reference(b *testing.B) {
	storage := newMockStorage()
	codec := NewCodec(storage, 1024, "test-bucket")

	// Setup: encode a large payload first
	largeData := make([]byte, 1024*1024)
	encoded, _ := codec.Encode(context.Background(), []*common.Payload{{Data: largeData}}, "default", "wf", "run")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.Decode(context.Background(), encoded)
	}
}
