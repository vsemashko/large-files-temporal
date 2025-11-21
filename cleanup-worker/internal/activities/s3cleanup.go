package activities

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/storage"
	"github.com/vsemashko/large-files-temporal/cleanup-worker/internal/workflows"
)

// S3CleanupActivities contains activities for S3 cleanup
type S3CleanupActivities struct {
	s3Client *storage.S3Client
}

// NewS3CleanupActivities creates a new S3CleanupActivities instance
func NewS3CleanupActivities(s3Client *storage.S3Client) *S3CleanupActivities {
	return &S3CleanupActivities{
		s3Client: s3Client,
	}
}

// DeleteS3Objects deletes S3 objects for a specific workflow
func (a *S3CleanupActivities) DeleteS3Objects(ctx context.Context, wf workflows.WorkflowInfo) (int, error) {
	logger := activity.GetLogger(ctx)

	// Construct S3 prefix for this workflow
	// Format: {namespace}/{workflow-id}/
	prefix := fmt.Sprintf("%s/%s/", wf.Namespace, wf.WorkflowID)

	logger.Info("Deleting S3 objects", "workflow", wf.WorkflowID, "prefix", prefix)

	// List objects with the prefix
	keys, err := a.s3Client.ListObjectsByPrefix(ctx, prefix)
	if err != nil {
		return 0, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	if len(keys) == 0 {
		logger.Info("No S3 objects found for workflow", "workflow", wf.WorkflowID)
		return 0, nil
	}

	logger.Info("Found S3 objects to delete", "workflow", wf.WorkflowID, "count", len(keys))

	// Delete objects
	deletedCount, err := a.s3Client.DeleteObjects(ctx, keys)
	if err != nil {
		return deletedCount, fmt.Errorf("failed to delete S3 objects: %w", err)
	}

	logger.Info("Deleted S3 objects", "workflow", wf.WorkflowID, "count", deletedCount)

	return deletedCount, nil
}
