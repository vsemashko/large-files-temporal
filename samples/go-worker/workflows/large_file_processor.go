package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// FileProcessingInput represents the input to the file processing workflow
type FileProcessingInput struct {
	FileData []byte            // Can be 10MB+ and will be stored in S3 by codec
	FileName string            // Name of the file
	Metadata map[string]string // Additional metadata
}

// FileProcessingResult represents the result of the file processing workflow
type FileProcessingResult struct {
	ProcessedSize  int64         // Size of processed data
	UploadURL      string        // URL where file was uploaded
	ProcessingTime time.Duration // Time taken to process
}

// LargeFileProcessor is a workflow that processes large files
// The codec will automatically store large payloads in S3
func LargeFileProcessor(ctx workflow.Context, input FileProcessingInput) (*FileProcessingResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting large file processing",
		"fileName", input.FileName,
		"size", len(input.FileData))

	startTime := workflow.Now(ctx)

	// Configure activity options
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Step 1: Process the large file
	// The fileData may be large (e.g., 10MB+), but the codec will handle it
	var processedData []byte
	err := workflow.ExecuteActivity(ctx, "ProcessLargeFile", input.FileData).Get(ctx, &processedData)
	if err != nil {
		logger.Error("Failed to process file", "error", err)
		return nil, err
	}

	logger.Info("File processed successfully", "processedSize", len(processedData))

	// Step 2: Upload to destination
	var uploadURL string
	err = workflow.ExecuteActivity(ctx, "UploadToDestination", processedData, input.FileName).Get(ctx, &uploadURL)
	if err != nil {
		logger.Error("Failed to upload file", "error", err)
		return nil, err
	}

	logger.Info("File uploaded successfully", "url", uploadURL)

	processingTime := workflow.Now(ctx).Sub(startTime)

	return &FileProcessingResult{
		ProcessedSize:  int64(len(processedData)),
		UploadURL:      uploadURL,
		ProcessingTime: processingTime,
	}, nil
}
