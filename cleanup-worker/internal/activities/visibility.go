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

	logger.Info("Checking archive status", "workflow", wf.WorkflowID)

	// Query the archive
	// Note: This requires Temporal's archival feature to be enabled
	req := &workflowservice.GetWorkflowExecutionHistoryRequest{
		Namespace: wf.Namespace,
		Execution: &enums.WorkflowExecution{
			WorkflowId: wf.WorkflowID,
			RunId:      wf.RunID,
		},
	}

	// Try to get the workflow from the archive
	iter := a.client.GetWorkflowHistory(ctx, wf.WorkflowID, wf.RunID, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)

	// If we can retrieve the history, the workflow might be archived
	// This is a simplified check - in production, you might want to check specific archival metadata
	hasHistory := iter.HasNext()

	if hasHistory {
		logger.Info("Workflow has history, may be archived", "workflow", wf.WorkflowID)
		// For now, we'll assume if we can get the history after the grace period, it's likely archived
		// In production, you'd check specific archival metadata or configuration
		return true, nil
	}

	logger.Info("Workflow does not appear to be archived", "workflow", wf.WorkflowID)
	return false, nil
}
