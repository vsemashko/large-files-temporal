package main

import (
	"context"
	"crypto/rand"
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"github.com/vsemashko/large-files-temporal/samples/go-worker/internal/codec"
	"github.com/vsemashko/large-files-temporal/samples/go-worker/workflows"
)

func main() {
	log.Println("Starting Temporal Client (Go)...")

	// Get configuration from environment
	temporalAddr := getEnv("TEMPORAL_ADDRESS", "localhost:7233")
	namespace := getEnv("TEMPORAL_NAMESPACE", "default")
	codecServerURL := getEnv("CODEC_SERVER_URL", "localhost:9090")
	taskQueue := getEnv("TASK_QUEUE", "large-files-task-queue")

	// Create remote codec
	remoteCodec, err := codec.NewRemotePayloadCodec(codecServerURL)
	if err != nil {
		log.Fatalf("Failed to create remote codec: %v", err)
	}
	defer remoteCodec.Close()

	// Create Temporal client with remote codec
	c, err := client.Dial(client.Options{
		HostPort:  temporalAddr,
		Namespace: namespace,
		DataConverter: converter.NewCodecDataConverter(
			converter.GetDefaultDataConverter(),
			remoteCodec,
		),
	})
	if err != nil {
		log.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer c.Close()

	// Generate a large file (5MB) to test the codec
	fileSize := 5 * 1024 * 1024 // 5MB
	largeFileData := make([]byte, fileSize)
	_, err = rand.Read(largeFileData)
	if err != nil {
		log.Fatalf("Failed to generate random data: %v", err)
	}

	log.Printf("Generated test file: %d bytes (%.2f MB)", fileSize, float64(fileSize)/1024/1024)

	// Create workflow input
	input := workflows.FileProcessingInput{
		FileData: largeFileData,
		FileName: "test-large-file.bin",
		Metadata: map[string]string{
			"contentType": "application/octet-stream",
			"source":      "go-client",
		},
	}

	// Start workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "large-file-workflow-" + randomString(8),
		TaskQueue: taskQueue,
	}

	log.Println("Starting workflow...")
	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, workflows.LargeFileProcessor, input)
	if err != nil {
		log.Fatalf("Unable to execute workflow: %v", err)
	}

	log.Printf("Started workflow: WorkflowID=%s, RunID=%s", we.GetID(), we.GetRunID())

	// Wait for workflow completion
	var result workflows.FileProcessingResult
	err = we.Get(context.Background(), &result)
	if err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	log.Printf("Workflow completed successfully!")
	log.Printf("  Processed Size: %d bytes (%.2f MB)", result.ProcessedSize, float64(result.ProcessedSize)/1024/1024)
	log.Printf("  Upload URL: %s", result.UploadURL)
	log.Printf("  Processing Time: %s", result.ProcessingTime)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}
