package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/activities"
	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/config"
	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/storage"
	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/workflows"
)

func main() {
	log.Println("Starting Temporal Cleanup Worker...")

	// Load configuration
	cfg := config.LoadConfig()
	log.Printf("Configuration loaded: gracePeriod=%d days, schedule=%s, checkArchive=%v",
		cfg.CleanupGracePeriodDays, cfg.CleanupSchedule, cfg.CheckArchiveBeforeDelete)

	if !cfg.CleanupEnabled {
		log.Println("Cleanup is disabled, exiting")
		return
	}

	ctx := context.Background()

	// Initialize S3 client
	s3Client, err := storage.NewS3Client(
		ctx,
		cfg.S3Bucket,
		cfg.S3Region,
		cfg.S3Endpoint,
		cfg.AWSAccessKeyID,
		cfg.AWSSecretAccessKey,
	)
	if err != nil {
		log.Fatalf("Failed to initialize S3 client: %v", err)
	}
	log.Println("S3 client initialized successfully")

	// Create Temporal client
	c, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		log.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer c.Close()

	log.Println("Connected to Temporal server")

	// Create worker
	w := worker.New(c, "cleanup-task-queue", worker.Options{})

	// Register workflows
	w.RegisterWorkflow(workflows.CleanupWorkflow)

	// Register activities
	visibilityActivities := activities.NewVisibilityActivities(c)
	w.RegisterActivity(visibilityActivities)

	s3CleanupActivities := activities.NewS3CleanupActivities(s3Client)
	w.RegisterActivity(s3CleanupActivities)

	log.Println("Worker registered on task queue: cleanup-task-queue")

	// Start worker in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- w.Run(worker.InterruptCh())
	}()

	log.Println("Worker started successfully")

	// Start a cleanup workflow on a schedule
	go scheduleCleanup(c, cfg)

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

// scheduleCleanup starts a cleanup workflow on a schedule
func scheduleCleanup(c client.Client, cfg *config.Config) {
	log.Printf("Scheduling cleanup workflow: %s", cfg.CleanupSchedule)

	// For simplicity, we'll run cleanup periodically
	// In production, use Temporal's Schedule feature or cron workflow
	ticker := time.NewTicker(24 * time.Hour) // Run daily
	defer ticker.Stop()

	// Run immediately on startup
	runCleanupWorkflow(c, cfg)

	for range ticker.C {
		runCleanupWorkflow(c, cfg)
	}
}

// runCleanupWorkflow starts a single cleanup workflow
func runCleanupWorkflow(c client.Client, cfg *config.Config) {
	ctx := context.Background()

	workflowOptions := client.StartWorkflowOptions{
		ID:        "cleanup-workflow-" + time.Now().Format("2006-01-02"),
		TaskQueue: "cleanup-task-queue",
	}

	cleanupConfig := workflows.CleanupConfig{
		GracePeriodDays:      cfg.CleanupGracePeriodDays,
		CheckArchiveEnabled:  cfg.CheckArchiveBeforeDelete,
		MaxWorkflowsPerBatch: cfg.MaxWorkflowsPerBatch,
	}

	log.Println("Starting cleanup workflow...")
	we, err := c.ExecuteWorkflow(ctx, workflowOptions, workflows.CleanupWorkflow, cleanupConfig)
	if err != nil {
		log.Printf("Failed to start cleanup workflow: %v", err)
		return
	}

	log.Printf("Started cleanup workflow: WorkflowID=%s, RunID=%s", we.GetID(), we.GetRunID())

	// Wait for completion
	err = we.Get(ctx, nil)
	if err != nil {
		log.Printf("Cleanup workflow failed: %v", err)
	} else {
		log.Println("Cleanup workflow completed successfully")
	}
}
