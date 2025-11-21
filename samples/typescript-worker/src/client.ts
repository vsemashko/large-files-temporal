import { Connection, Client } from '@temporalio/client';
import { DefaultPayloadConverter } from '@temporalio/common';
import { RemoteCodec } from './codec/remote-codec';
import { largeFileProcessor } from './workflows/large-file-processor';
import type { FileProcessingInput } from './workflows/large-file-processor';
import crypto from 'crypto';

async function run() {
  console.log('Starting Temporal Client (TypeScript)...');

  // Get configuration from environment
  const temporalAddress = process.env.TEMPORAL_ADDRESS || 'localhost:7233';
  const namespace = process.env.TEMPORAL_NAMESPACE || 'default';
  const codecServerUrl =
    process.env.CODEC_SERVER_URL || 'http://localhost:8080';
  const taskQueue = process.env.TASK_QUEUE || 'large-files-task-queue';

  // Create connection to Temporal
  const connection = await Connection.connect({
    address: temporalAddress,
  });

  // Create client with remote codec
  const remoteCodec = new RemoteCodec(codecServerUrl);
  const client = new Client({
    connection,
    namespace,
    dataConverter: {
      payloadCodec: remoteCodec,
    },
  });

  console.log('Connected to Temporal server with remote codec');

  // Generate a large file (5MB) to test the codec
  const fileSize = 5 * 1024 * 1024; // 5MB
  const largeFileData = crypto.randomBytes(fileSize);

  console.log(
    `Generated test file: ${fileSize} bytes (${(
      fileSize /
      1024 /
      1024
    ).toFixed(2)} MB)`
  );

  // Create workflow input
  const input: FileProcessingInput = {
    fileData: largeFileData,
    fileName: 'test-large-file.bin',
    metadata: {
      contentType: 'application/octet-stream',
      source: 'typescript-client',
    },
  };

  // Start workflow
  const workflowId = `large-file-workflow-${randomString(8)}`;
  console.log(`Starting workflow: ${workflowId}`);

  const handle = await client.workflow.start(largeFileProcessor, {
    taskQueue,
    workflowId,
    args: [input],
  });

  console.log(
    `Started workflow: WorkflowID=${handle.workflowId}, RunID=${handle.firstExecutionRunId}`
  );

  // Wait for workflow completion
  const result = await handle.result();

  console.log('Workflow completed successfully!');
  console.log(
    `  Processed Size: ${result.processedSize} bytes (${(
      result.processedSize /
      1024 /
      1024
    ).toFixed(2)} MB)`
  );
  console.log(`  Upload URL: ${result.uploadURL}`);
  console.log(`  Processing Time: ${result.processingTimeMs}ms`);
}

function randomString(length: number): string {
  const chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

run().catch((err) => {
  console.error('Client error:', err);
  process.exit(1);
});
