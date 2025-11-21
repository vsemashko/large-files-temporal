/**
 * File processing activities
 */

/**
 * Processes a large file
 * This activity receives the file data (which may have been stored in S3 by the codec)
 */
export async function processLargeFile(fileData: Buffer): Promise<Buffer> {
  console.log(`Processing file of size: ${fileData.length} bytes`);

  // Simulate file processing (e.g., image processing, video encoding, data transformation)
  // In a real application, this would do actual work
  await sleep(2000);

  console.log(`File processing completed (${fileData.length} bytes)`);

  // For demo purposes, return the same data
  // In real scenarios, this might return processed/transformed data
  return fileData;
}

/**
 * Uploads the processed file to a destination
 */
export async function uploadToDestination(
  data: Buffer,
  fileName: string
): Promise<string> {
  console.log(
    `Uploading file to destination: ${fileName} (${data.length} bytes)`
  );

  // Simulate upload to final destination (S3, CDN, database, etc.)
  // In a real application, this would do actual upload
  await sleep(1000);

  const uploadURL = `https://destination.example.com/${fileName}-${Date.now()}`;

  console.log(`File uploaded successfully: ${uploadURL}`);

  return uploadURL;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
