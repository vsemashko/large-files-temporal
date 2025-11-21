package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Storage defines the interface for storing and retrieving payloads
type Storage interface {
	Upload(ctx context.Context, key string, data []byte, metadata map[string]string) error
	Download(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
	ListObjects(ctx context.Context, prefix string) ([]string, error)
}

// S3Storage implements Storage interface using AWS S3
type S3Storage struct {
	client *s3.Client
	bucket string
}

// NewS3Storage creates a new S3Storage instance
func NewS3Storage(ctx context.Context, bucket, region, endpoint, accessKeyID, secretAccessKey string) (*S3Storage, error) {
	var cfg aws.Config
	var err error

	if accessKeyID != "" && secretAccessKey != "" {
		// Use provided credentials
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				accessKeyID,
				secretAccessKey,
				"",
			)),
		)
	} else {
		// Use default credentials (IAM role, env vars, etc.)
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	var s3Client *s3.Client
	if endpoint != "" {
		// Custom endpoint (LocalStack, MinIO, etc.)
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // Required for LocalStack/MinIO
		})
	} else {
		s3Client = s3.NewFromConfig(cfg)
	}

	// Ensure bucket exists
	storage := &S3Storage{
		client: s3Client,
		bucket: bucket,
	}

	if err := storage.ensureBucket(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	return storage, nil
}

// ensureBucket creates the bucket if it doesn't exist
func (s *S3Storage) ensureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err == nil {
		return nil // Bucket exists
	}

	// Create bucket
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	return nil
}

// Upload uploads data to S3
func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, metadata map[string]string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		Body:     bytes.NewReader(data),
		Metadata: metadata,
		ServerSideEncryption: types.ServerSideEncryptionAes256,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Download downloads data from S3
func (s *S3Storage) Download(ctx context.Context, key string) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object: %w", err)
	}

	return data, nil
}

// Delete deletes an object from S3
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// DeletePrefix deletes all objects with the given prefix
func (s *S3Storage) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	// List objects with prefix
	keys, err := s.ListObjects(ctx, prefix)
	if err != nil {
		return 0, err
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// Prepare objects for deletion
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = types.ObjectIdentifier{Key: aws.String(key)}
	}

	// Delete objects
	_, err = s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{Objects: objects},
	})

	if err != nil {
		return 0, fmt.Errorf("failed to delete objects: %w", err)
	}

	return len(keys), nil
}

// ListObjects lists all objects with the given prefix
func (s *S3Storage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	keys := make([]string, len(result.Contents))
	for i, obj := range result.Contents {
		keys[i] = *obj.Key
	}

	return keys, nil
}
