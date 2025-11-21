import { proxyActivities } from '@temporalio/workflow';
import type * as activities from '../activities';

// Configure activity options
const { processLargeFile, uploadToDestination } = proxyActivities<
  typeof activities
>({
  startToCloseTimeout: '10 minutes',
  retry: {
    initialInterval: '1s',
    backoffCoefficient: 2,
    maximumInterval: '1m',
    maximumAttempts: 3,
  },
});

export interface FileProcessingInput {
  fileData: Buffer; // Can be 10MB+ and will be stored in S3 by codec
  fileName: string;
  metadata?: Record<string, string>;
}

export interface FileProcessingResult {
  processedSize: number;
  uploadURL: string;
  processingTimeMs: number;
}

/**
 * LargeFileProcessor workflow processes large files
 * The codec will automatically store large payloads in S3
 */
export async function largeFileProcessor(
  input: FileProcessingInput
): Promise<FileProcessingResult> {
  const startTime = Date.now();

  console.log(
    `Starting large file processing: ${input.fileName} (${input.fileData.length} bytes)`
  );

  // Step 1: Process the large file
  // The fileData may be large (e.g., 10MB+), but the codec will handle it
  const processedData = await processLargeFile(input.fileData);

  console.log(`File processed successfully (${processedData.length} bytes)`);

  // Step 2: Upload to destination
  const uploadURL = await uploadToDestination(processedData, input.fileName);

  console.log(`File uploaded successfully: ${uploadURL}`);

  const processingTimeMs = Date.now() - startTime;

  return {
    processedSize: processedData.length,
    uploadURL,
    processingTimeMs,
  };
}
