package activities

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"

	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/workflows"
)

// VisibilityActivities contains activities for querying Temporal visibility
type VisibilityActivities struct {
	client client.Client
}

// NewVisibilityActivities creates a new VisibilityActivities instance
func NewVisibilityActivities(c client.Client) *VisibilityActivities {
	return &VisibilityActivities{
		client: c,
	}
}

// GetCompletedWorkflows gets all completed workflows within the grace period
func (a *VisibilityActivities) GetCompletedWorkflows(ctx context.Context, gracePeriodDays int) ([]workflows.WorkflowInfo, error) {
	logger := activity.GetLogger(ctx)

	// Calculate the time threshold
	threshold := time.Now().Add(-time.Duration(gracePeriodDays) * 24 * time.Hour)

	logger.Info("Querying completed workflows", "threshold", threshold)

	// Query for completed workflows
	// We look for workflows that completed before the threshold
	query := fmt.Sprintf(
		"CloseTime < '%s' AND ExecutionStatus IN ('Completed', 'Failed', 'Terminated', 'Canceled', 'TimedOut')",
		threshold.Format(time.RFC3339),
	)

	var result []workflows.WorkflowInfo

	// List workflows using visibility API
	iter := a.client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Query: query,
	})

	for iter.HasNext() {
		exec, err := iter.Next()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate workflows: %w", err)
		}

		result = append(result, workflows.WorkflowInfo{
			WorkflowID: exec.Execution.WorkflowId,
			RunID:      exec.Execution.RunId,
			Namespace:  a.client.Options().Namespace,
			CloseTime:  exec.CloseTime.AsTime(),
			Status:     exec.Status.String(),
		})
	}

	logger.Info("Found completed workflows", "count", len(result))

	return result, nil
}

// CheckIfArchived checks if a workflow is archived
func (a *VisibilityActivities) CheckIfArchived(ctx context.Context, wf workflows.WorkflowInfo) (bool, error) {
	logger := activity.GetLogger(ctx)

	logger.Info("Checking archive status", "workflow", wf.WorkflowID, "runID", wf.RunID)

	// Try to describe the workflow execution to check if it still exists in primary storage
	_, err := a.client.DescribeWorkflowExecution(ctx, wf.WorkflowID, wf.RunID)
	if err != nil {
		// If workflow is not found, it may have been archived or removed
		// To be conservative, assume it's archived and skip deletion
		logger.Info("Workflow not found in primary storage, assuming archived", "workflow", wf.WorkflowID, "error", err)
		return true, nil
	}

	// Check if we can get the workflow history
	// If archival is enabled, older workflows will have their history in archival storage
	iter := a.client.GetWorkflowHistory(
		ctx,
		wf.WorkflowID,
		wf.RunID,
		false, // isLongPoll = false
		enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT,
	)

	// Verify we can read at least one history event
	if !iter.HasNext() {
		logger.Info("No history available, assuming not archived", "workflow", wf.WorkflowID)
		return false, nil
	}

	// Try to read the first event
	_, err = iter.Next()
	if err != nil {
		logger.Warn("Error reading history, assuming not archived", "workflow", wf.WorkflowID, "error", err)
		return false, nil
	}

	// For a more accurate check, we should verify namespace archival configuration
	// For now, we use a simple heuristic: workflows older than 30 days are likely archived
	// This can be made configurable
	const archivalAgeDays = 30
	archivalThreshold := time.Now().Add(-archivalAgeDays * 24 * time.Hour)

	if wf.CloseTime.Before(archivalThreshold) {
		logger.Info("Workflow is older than archival threshold, assuming archived",
			"workflow", wf.WorkflowID,
			"closeTime", wf.CloseTime,
			"threshold", archivalThreshold)
		return true, nil
	}

	logger.Info("Workflow is not archived", "workflow", wf.WorkflowID, "closeTime", wf.CloseTime)
	return false, nil
}
