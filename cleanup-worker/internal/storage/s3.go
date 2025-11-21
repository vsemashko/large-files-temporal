package storage

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Client wraps S3 operations
type S3Client struct {
	client *s3.Client
	bucket string
}

// NewS3Client creates a new S3 client
func NewS3Client(ctx context.Context, bucket, region, endpoint, accessKeyID, secretAccessKey string) (*S3Client, error) {
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

	return &S3Client{
		client: s3Client,
		bucket: bucket,
	}, nil
}

// ListObjectsByPrefix lists all objects with the given prefix
func (c *S3Client) ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	var allKeys []string
	var continuationToken *string

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		}

		result, err := c.client.ListObjectsV2(ctx, input)
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

// DeleteObjects deletes multiple objects
func (c *S3Client) DeleteObjects(ctx context.Context, keys []string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	// Prepare objects for deletion
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = types.ObjectIdentifier{Key: aws.String(key)}
	}

	// Delete objects
	_, err := c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(c.bucket),
		Delete: &types.Delete{Objects: objects},
	})

	if err != nil {
		return 0, fmt.Errorf("failed to delete objects: %w", err)
	}

	return len(keys), nil
}

// DeleteByPrefix deletes all objects with the given prefix
func (c *S3Client) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	keys, err := c.ListObjectsByPrefix(ctx, prefix)
	if err != nil {
		return 0, err
	}

	return c.DeleteObjects(ctx, keys)
}
