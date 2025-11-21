package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"

	"github.com/vsemashko/large-files-temporal/samples/go-worker/activities"
	"github.com/vsemashko/large-files-temporal/samples/go-worker/internal/codec"
	"github.com/vsemashko/large-files-temporal/samples/go-worker/workflows"
)

func main() {
	log.Println("Starting Temporal Worker (Go)...")

	// Get configuration from environment
	temporalAddr := getEnv("TEMPORAL_ADDRESS", "localhost:7233")
	namespace := getEnv("TEMPORAL_NAMESPACE", "default")
	codecServerURL := getEnv("CODEC_SERVER_URL", "localhost:9090")
	taskQueue := getEnv("TASK_QUEUE", "large-files-task-queue")

	log.Printf("Configuration: temporal=%s, namespace=%s, codec=%s, taskQueue=%s",
		temporalAddr, namespace, codecServerURL, taskQueue)

	// Create remote codec
	remoteCodec, err := codec.NewRemotePayloadCodec(codecServerURL)
	if err != nil {
		log.Fatalf("Failed to create remote codec: %v", err)
	}
	defer remoteCodec.Close()

	log.Println("Connected to codec server")

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

	log.Println("Connected to Temporal server")

	// Create worker
	w := worker.New(c, taskQueue, worker.Options{})

	// Register workflows and activities
	w.RegisterWorkflow(workflows.LargeFileProcessor)
	w.RegisterActivity(&activities.FileProcessingActivities{})

	log.Printf("Worker registered on task queue: %s", taskQueue)

	// Start worker in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- w.Run(worker.InterruptCh())
	}()

	log.Println("Worker started successfully")

	// Wait for interrupt signal or worker error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Received interrupt signal, shutting down...")
	case err := <-errChan:
		log.Printf("Worker error: %v", err)
	}

	log.Println("Worker stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
