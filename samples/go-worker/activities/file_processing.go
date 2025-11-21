package activities

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
)

// FileProcessingActivities contains activities for file processing
type FileProcessingActivities struct{}

// ProcessLargeFile processes a large file
// This activity receives the file data (which may have been stored in S3 by the codec)
func (a *FileProcessingActivities) ProcessLargeFile(ctx context.Context, fileData []byte) ([]byte, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Processing file", "size", len(fileData))

	// Simulate file processing (e.g., image processing, video encoding, data transformation)
	// In a real application, this would do actual work
	time.Sleep(2 * time.Second)

	logger.Info("File processing completed", "size", len(fileData))

	// For demo purposes, return the same data
	// In real scenarios, this might return processed/transformed data
	return fileData, nil
}

// UploadToDestination uploads the processed file to a destination
func (a *FileProcessingActivities) UploadToDestination(ctx context.Context, data []byte, fileName string) (string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Uploading file to destination", "fileName", fileName, "size", len(data))

	// Simulate upload to final destination (S3, CDN, database, etc.)
	// In a real application, this would do actual upload
	time.Sleep(1 * time.Second)

	uploadURL := fmt.Sprintf("https://destination.example.com/%s-%d", fileName, time.Now().Unix())

	logger.Info("File uploaded successfully", "url", uploadURL)

	return uploadURL, nil
}
