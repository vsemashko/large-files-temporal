package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// CleanupConfig contains configuration for the cleanup workflow
type CleanupConfig struct {
	GracePeriodDays      int
	CheckArchiveEnabled  bool
	MaxWorkflowsPerBatch int
}

// CleanupWorkflow periodically cleans up S3 objects for completed workflows
func CleanupWorkflow(ctx workflow.Context, config CleanupConfig) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting cleanup workflow", "config", config)

	// Configure activity options
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Get completed workflows
	var completedWorkflows []WorkflowInfo
	err := workflow.ExecuteActivity(ctx, "GetCompletedWorkflows", config.GracePeriodDays).Get(ctx, &completedWorkflows)
	if err != nil {
		logger.Error("Failed to get completed workflows", "error", err)
		return err
	}

	logger.Info("Found workflows to process", "count", len(completedWorkflows))

	if len(completedWorkflows) == 0 {
		logger.Info("No workflows to clean up")
		return nil
	}

	// Process each workflow
	totalDeleted := 0
	totalSkipped := 0
	totalErrors := 0

	for _, wf := range completedWorkflows {
		// Check if archived (if enabled)
		if config.CheckArchiveEnabled {
			var isArchived bool
			err := workflow.ExecuteActivity(ctx, "CheckIfArchived", wf).Get(ctx, &isArchived)
			if err != nil {
				logger.Error("Failed to check archive status", "workflow", wf.WorkflowID, "error", err)
				totalErrors++
				continue
			}

			if isArchived {
				logger.Info("Skipping archived workflow", "workflow", wf.WorkflowID)
				totalSkipped++
				continue
			}
		}

		// Delete S3 objects
		var deletedCount int
		err := workflow.ExecuteActivity(ctx, "DeleteS3Objects", wf).Get(ctx, &deletedCount)
		if err != nil {
			logger.Error("Failed to delete S3 objects", "workflow", wf.WorkflowID, "error", err)
			totalErrors++
			continue
		}

		if deletedCount > 0 {
			logger.Info("Cleaned up workflow", "workflow", wf.WorkflowID, "deleted", deletedCount)
			totalDeleted += deletedCount
		}
	}

	logger.Info("Cleanup completed",
		"totalWorkflows", len(completedWorkflows),
		"totalDeleted", totalDeleted,
		"totalSkipped", totalSkipped,
		"totalErrors", totalErrors)

	return nil
}

// WorkflowInfo contains information about a workflow
type WorkflowInfo struct {
	WorkflowID string
	RunID      string
	Namespace  string
	CloseTime  time.Time
	Status     string
}
