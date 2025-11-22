package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Mock S3 client for testing
type mockS3Client struct {
	objects          map[string][]byte
	metadata         map[string]map[string]string
	headBucketErr    error
	createBucketErr  error
	putObjectErr     error
	getObjectErr     error
	deleteObjectErr  error
	deleteObjectsErr error
	listObjectsErr   error
	listObjectsPages [][]types.Object // For pagination testing
}

func (m *mockS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, m.headBucketErr
}

func (m *mockS3Client) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, m.createBucketErr
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putObjectErr != nil {
		return nil, m.putObjectErr
	}

	// Read data from body
	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}

	// Store object
	m.objects[*params.Key] = data
	m.metadata[*params.Key] = params.Metadata

	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getObjectErr != nil {
		return nil, m.getObjectErr
	}

	data, exists := m.objects[*params.Key]
	if !exists {
		return nil, &types.NoSuchKey{Message: aws.String("key not found")}
	}

	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectErr != nil {
		return nil, m.deleteObjectErr
	}

	delete(m.objects, *params.Key)
	delete(m.metadata, *params.Key)

	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	if m.deleteObjectsErr != nil {
		return nil, m.deleteObjectsErr
	}

	for _, obj := range params.Delete.Objects {
		delete(m.objects, *obj.Key)
		delete(m.metadata, *obj.Key)
	}

	return &s3.DeleteObjectsOutput{}, nil
}

func (m *mockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.listObjectsErr != nil {
		return nil, m.listObjectsErr
	}

	// Handle pagination for testing
	if len(m.listObjectsPages) > 0 {
		pageNum := 0
		if params.ContinuationToken != nil {
			// Simple pagination: token is page number as string
			if *params.ContinuationToken == "page1" {
				pageNum = 1
			} else if *params.ContinuationToken == "page2" {
				pageNum = 2
			}
		}

		if pageNum < len(m.listObjectsPages) {
			output := &s3.ListObjectsV2Output{
				Contents: m.listObjectsPages[pageNum],
			}

			// Set continuation token if more pages exist
			if pageNum+1 < len(m.listObjectsPages) {
				output.IsTruncated = aws.Bool(true)
				if pageNum == 0 {
					output.NextContinuationToken = aws.String("page1")
				} else if pageNum == 1 {
					output.NextContinuationToken = aws.String("page2")
				}
			}

			return output, nil
		}
	}

	// Non-paginated: return all objects matching prefix
	var contents []types.Object
	prefix := ""
	if params.Prefix != nil {
		prefix = *params.Prefix
	}

	for key := range m.objects {
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			contents = append(contents, types.Object{
				Key: aws.String(key),
			})
		}
	}

	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(false),
	}, nil
}

func TestUpload_Success(t *testing.T) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	data := []byte("test payload data")
	metadata := map[string]string{
		"workflow-id": "test-workflow",
		"run-id":      "test-run",
	}

	err := storage.Upload(context.Background(), "test-key", data, metadata)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Verify data was stored
	storedData, exists := mock.objects["test-key"]
	if !exists {
		t.Fatal("Object was not stored")
	}

	if !bytes.Equal(storedData, data) {
		t.Errorf("Stored data mismatch: got %v, want %v", storedData, data)
	}

	// Verify metadata was stored
	storedMetadata := mock.metadata["test-key"]
	if storedMetadata["workflow-id"] != "test-workflow" {
		t.Errorf("Metadata mismatch: got %v, want test-workflow", storedMetadata["workflow-id"])
	}
}

func TestUpload_Error(t *testing.T) {
	mock := &mockS3Client{
		objects:      make(map[string][]byte),
		metadata:     make(map[string]map[string]string),
		putObjectErr: errors.New("S3 upload failed"),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	err := storage.Upload(context.Background(), "test-key", []byte("data"), nil)
	if err == nil {
		t.Fatal("Expected upload to fail")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("S3 upload failed")) {
		t.Errorf("Expected error to contain 'S3 upload failed', got: %v", err)
	}
}

func TestDownload_Success(t *testing.T) {
	testData := []byte("test payload data")
	mock := &mockS3Client{
		objects: map[string][]byte{
			"test-key": testData,
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	data, err := storage.Download(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if !bytes.Equal(data, testData) {
		t.Errorf("Downloaded data mismatch: got %v, want %v", data, testData)
	}
}

func TestDownload_NotFound(t *testing.T) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	_, err := storage.Download(context.Background(), "non-existent-key")
	if err == nil {
		t.Fatal("Expected download to fail for non-existent key")
	}
}

func TestDownload_Error(t *testing.T) {
	mock := &mockS3Client{
		objects:      make(map[string][]byte),
		metadata:     make(map[string]map[string]string),
		getObjectErr: errors.New("S3 download failed"),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	_, err := storage.Download(context.Background(), "test-key")
	if err == nil {
		t.Fatal("Expected download to fail")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("S3 download failed")) {
		t.Errorf("Expected error to contain 'S3 download failed', got: %v", err)
	}
}

func TestDelete_Success(t *testing.T) {
	mock := &mockS3Client{
		objects: map[string][]byte{
			"test-key": []byte("data"),
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	err := storage.Delete(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify object was deleted
	if _, exists := mock.objects["test-key"]; exists {
		t.Error("Object was not deleted")
	}
}

func TestDelete_Error(t *testing.T) {
	mock := &mockS3Client{
		objects:         make(map[string][]byte),
		metadata:        make(map[string]map[string]string),
		deleteObjectErr: errors.New("S3 delete failed"),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	err := storage.Delete(context.Background(), "test-key")
	if err == nil {
		t.Fatal("Expected delete to fail")
	}
}

func TestDeletePrefix_Success(t *testing.T) {
	mock := &mockS3Client{
		objects: map[string][]byte{
			"prefix/file1": []byte("data1"),
			"prefix/file2": []byte("data2"),
			"prefix/file3": []byte("data3"),
			"other/file":   []byte("data4"),
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	count, err := storage.DeletePrefix(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected to delete 3 objects, got %d", count)
	}

	// Verify prefix objects were deleted
	if _, exists := mock.objects["prefix/file1"]; exists {
		t.Error("prefix/file1 was not deleted")
	}
	if _, exists := mock.objects["prefix/file2"]; exists {
		t.Error("prefix/file2 was not deleted")
	}
	if _, exists := mock.objects["prefix/file3"]; exists {
		t.Error("prefix/file3 was not deleted")
	}

	// Verify other objects remain
	if _, exists := mock.objects["other/file"]; !exists {
		t.Error("other/file was incorrectly deleted")
	}
}

func TestDeletePrefix_EmptyResult(t *testing.T) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	count, err := storage.DeletePrefix(context.Background(), "non-existent/")
	if err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected to delete 0 objects, got %d", count)
	}
}

func TestListObjects_Success(t *testing.T) {
	mock := &mockS3Client{
		objects: map[string][]byte{
			"prefix/file1": []byte("data1"),
			"prefix/file2": []byte("data2"),
			"other/file":   []byte("data3"),
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	keys, err := storage.ListObjects(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Expected 2 objects, got %d", len(keys))
	}

	// Verify keys are correct
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	if !keyMap["prefix/file1"] || !keyMap["prefix/file2"] {
		t.Errorf("Expected keys prefix/file1 and prefix/file2, got %v", keys)
	}
}

func TestListObjects_Pagination(t *testing.T) {
	// Setup mock with paginated results
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
		listObjectsPages: [][]types.Object{
			// Page 0
			{
				{Key: aws.String("prefix/file1")},
				{Key: aws.String("prefix/file2")},
			},
			// Page 1
			{
				{Key: aws.String("prefix/file3")},
				{Key: aws.String("prefix/file4")},
			},
			// Page 2
			{
				{Key: aws.String("prefix/file5")},
			},
		},
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	keys, err := storage.ListObjects(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}

	// Should get all keys across all pages
	if len(keys) != 5 {
		t.Errorf("Expected 5 objects across all pages, got %d", len(keys))
	}

	expectedKeys := map[string]bool{
		"prefix/file1": false,
		"prefix/file2": false,
		"prefix/file3": false,
		"prefix/file4": false,
		"prefix/file5": false,
	}

	for _, key := range keys {
		if _, exists := expectedKeys[key]; exists {
			expectedKeys[key] = true
		}
	}

	for key, found := range expectedKeys {
		if !found {
			t.Errorf("Expected key %s not found in results", key)
		}
	}
}

func TestListObjects_Error(t *testing.T) {
	mock := &mockS3Client{
		objects:        make(map[string][]byte),
		metadata:       make(map[string]map[string]string),
		listObjectsErr: errors.New("S3 list failed"),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	_, err := storage.ListObjects(context.Background(), "prefix/")
	if err == nil {
		t.Fatal("Expected ListObjects to fail")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("S3 list failed")) {
		t.Errorf("Expected error to contain 'S3 list failed', got: %v", err)
	}
}

func TestListObjects_EmptyPrefix(t *testing.T) {
	mock := &mockS3Client{
		objects: map[string][]byte{
			"file1": []byte("data1"),
			"file2": []byte("data2"),
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	keys, err := storage.ListObjects(context.Background(), "")
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Expected 2 objects, got %d", len(keys))
	}
}

func TestUploadDownloadRoundtrip(t *testing.T) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	testData := []byte("round trip test data with special chars: 日本語, émojis 🎉")
	testKey := "test/roundtrip/key"
	testMetadata := map[string]string{
		"content-type": "application/octet-stream",
		"workflow-id":  "test-workflow-123",
	}

	// Upload
	err := storage.Upload(context.Background(), testKey, testData, testMetadata)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Download
	downloadedData, err := storage.Download(context.Background(), testKey)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify data integrity
	if !bytes.Equal(downloadedData, testData) {
		t.Errorf("Round trip data mismatch:\nOriginal:   %v\nDownloaded: %v", testData, downloadedData)
	}

	// Verify metadata was stored
	storedMetadata := mock.metadata[testKey]
	if storedMetadata["workflow-id"] != "test-workflow-123" {
		t.Errorf("Metadata not preserved: got %v", storedMetadata)
	}
}

func BenchmarkUpload_1KB(b *testing.B) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	data := make([]byte, 1024) // 1KB
	metadata := map[string]string{"test": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Upload(context.Background(), "bench-key", data, metadata)
	}
}

func BenchmarkUpload_1MB(b *testing.B) {
	mock := &mockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	data := make([]byte, 1024*1024) // 1MB
	metadata := map[string]string{"test": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = storage.Upload(context.Background(), "bench-key", data, metadata)
	}
}

func BenchmarkDownload_1KB(b *testing.B) {
	data := make([]byte, 1024) // 1KB
	mock := &mockS3Client{
		objects: map[string][]byte{
			"bench-key": data,
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Download(context.Background(), "bench-key")
	}
}

func BenchmarkDownload_1MB(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	mock := &mockS3Client{
		objects: map[string][]byte{
			"bench-key": data,
		},
		metadata: make(map[string]map[string]string),
	}

	storage := &S3Storage{
		client: mock,
		bucket: "test-bucket",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.Download(context.Background(), "bench-key")
	}
}
