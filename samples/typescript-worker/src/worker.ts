import { NativeConnection, Worker } from '@temporalio/worker';
import { DefaultPayloadConverterWithProtobufs } from '@temporalio/common';
import * as activities from './activities/file-processing';
import { RemoteCodec } from './codec/remote-codec';

async function run() {
  console.log('Starting Temporal Worker (TypeScript)...');

  // Get configuration from environment
  const temporalAddress = process.env.TEMPORAL_ADDRESS || 'localhost:7233';
  const namespace = process.env.TEMPORAL_NAMESPACE || 'default';
  const codecServerUrl =
    process.env.CODEC_SERVER_URL || 'http://localhost:8080';
  const taskQueue = process.env.TASK_QUEUE || 'large-files-task-queue';

  console.log(`Configuration:
    Temporal: ${temporalAddress}
    Namespace: ${namespace}
    Codec Server: ${codecServerUrl}
    Task Queue: ${taskQueue}
  `);

  // Create connection to Temporal
  const connection = await NativeConnection.connect({
    address: temporalAddress,
  });

  console.log('Connected to Temporal server');

  // Create worker with remote codec
  const worker = await Worker.create({
    connection,
    namespace,
    taskQueue,
    workflowsPath: require.resolve('./workflows'),
    activities,
    dataConverter: {
      payloadConverterPath: require.resolve('./codec/payload-converter'),
    },
  });

  console.log(`Worker registered on task queue: ${taskQueue}`);

  // Run the worker
  await worker.run();
}

run().catch((err) => {
  console.error('Worker error:', err);
  process.exit(1);
});
