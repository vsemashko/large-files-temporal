# Cleanup Worker

Temporal worker that periodically cleans up S3 objects for completed non-archived workflows.

## Features

- Scheduled cleanup workflow
- Configurable grace period
- Archive detection
- Batch processing
- Error handling and retry logic

## Building

```bash
make build
```

## Running

```bash
export TEMPORAL_ADDRESS=localhost:7233
export S3_BUCKET=temporal-large-payloads
export S3_ENDPOINT=http://localhost:4566
export CLEANUP_GRACE_PERIOD_DAYS=7
make run
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CLEANUP_GRACE_PERIOD_DAYS` | Days before cleanup | 7 |
| `CHECK_ARCHIVE_BEFORE_DELETE` | Check archive | true |
| `CLEANUP_SCHEDULE` | Cron schedule | 0 2 * * * |
| `MAX_WORKFLOWS_PER_BATCH` | Batch size | 100 |

## Workflow

The `CleanupWorkflow`:
1. Queries Temporal for completed workflows older than grace period
2. For each workflow:
   - Checks if archived (optional)
   - Deletes S3 objects if not archived
3. Logs statistics

## Activities

- `GetCompletedWorkflows`: Queries Temporal visibility API
- `CheckIfArchived`: Checks if workflow is archived
- `DeleteS3Objects`: Deletes S3 objects for workflow

## Scheduling

The worker automatically starts a cleanup workflow:
- On startup (immediate run)
- Periodically (configurable schedule)

## Testing

```bash
# Test with 0 grace period (immediate cleanup)
export CLEANUP_GRACE_PERIOD_DAYS=0
make run
```

## Docker

```bash
make docker-build
docker run -e TEMPORAL_ADDRESS=temporal:7233 \
           -e S3_ENDPOINT=http://localstack:4566 \
           temporal-cleanup-worker:latest
```
