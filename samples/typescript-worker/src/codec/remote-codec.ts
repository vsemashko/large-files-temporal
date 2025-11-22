import { PayloadCodec } from '@temporalio/common';
import type { Payload } from '@temporalio/common';

/**
 * RemoteCodec communicates with the codec server via HTTP
 * For TypeScript workers, we use HTTP instead of gRPC for simplicity
 */
export class RemoteCodec implements PayloadCodec {
  constructor(private readonly codecServerUrl: string) {}

  async encode(payloads: Payload[]): Promise<Payload[]> {
    if (payloads.length === 0) {
      return payloads;
    }

    try {
      // Extract workflow context from payload metadata
      const workflowContext = this.extractWorkflowContext(payloads);

      // Serialize payloads to base64 for HTTP transport
      const serializedPayloads = payloads.map((p) => ({
        metadata: Object.fromEntries(
          Object.entries(p.metadata || {}).map(([k, v]) => [
            k,
            Buffer.from(v).toString('base64'),
          ])
        ),
        data: p.data ? Buffer.from(p.data).toString('base64') : undefined,
      }));

      const response = await fetch(`${this.codecServerUrl}/encode`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          payloads: serializedPayloads,
          namespace: workflowContext.namespace,
          workflowId: workflowContext.workflowId,
          runId: workflowContext.runId,
        }),
      });

      if (!response.ok) {
        throw new Error(
          `Codec server encode failed: ${response.status} ${response.statusText}`
        );
      }

      const result = await response.json();

      // Deserialize response
      const encoded = result.payloads.map((p: any) => ({
        metadata: Object.fromEntries(
          Object.entries(p.metadata || {}).map(([k, v]) => [
            k,
            Buffer.from(v as string, 'base64'),
          ])
        ),
        data: p.data ? Buffer.from(p.data as string, 'base64') : undefined,
      }));

      console.log(`Encoded ${encoded.length} payloads via remote codec`);
      return encoded;
    } catch (error) {
      console.error('Failed to encode payloads:', error);
      // Fallback to original payloads on error
      return payloads;
    }
  }

  async decode(payloads: Payload[]): Promise<Payload[]> {
    if (payloads.length === 0) {
      return payloads;
    }

    try {
      // Serialize payloads to base64 for HTTP transport
      const serializedPayloads = payloads.map((p) => ({
        metadata: Object.fromEntries(
          Object.entries(p.metadata || {}).map(([k, v]) => [
            k,
            Buffer.from(v).toString('base64'),
          ])
        ),
        data: p.data ? Buffer.from(p.data).toString('base64') : undefined,
      }));

      const response = await fetch(`${this.codecServerUrl}/decode`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          payloads: serializedPayloads,
        }),
      });

      if (!response.ok) {
        throw new Error(
          `Codec server decode failed: ${response.status} ${response.statusText}`
        );
      }

      const result = await response.json();

      // Deserialize response
      const decoded = result.payloads.map((p: any) => ({
        metadata: Object.fromEntries(
          Object.entries(p.metadata || {}).map(([k, v]) => [
            k,
            Buffer.from(v as string, 'base64'),
          ])
        ),
        data: p.data ? Buffer.from(p.data as string, 'base64') : undefined,
      }));

      console.log(`Decoded ${decoded.length} payloads via remote codec`);
      return decoded;
    } catch (error) {
      console.error('Failed to decode payloads:', error);
      // Fallback to original payloads on error
      return payloads;
    }
  }

  /**
   * Extract workflow context from payload metadata
   * Temporal payloads may contain metadata with workflow information
   */
  private extractWorkflowContext(payloads: Payload[]): {
    namespace: string;
    workflowId: string;
    runId: string;
  } {
    const context = {
      namespace: 'default',
      workflowId: 'unknown',
      runId: 'unknown',
    };

    // Try to extract context from payload metadata
    // Temporal may embed workflow context in payload metadata
    for (const payload of payloads) {
      if (!payload.metadata) continue;

      // Check for temporal-workflow-id
      if (payload.metadata['temporal-workflow-id']) {
        context.workflowId = Buffer.from(
          payload.metadata['temporal-workflow-id']
        ).toString('utf-8');
      }

      // Check for temporal-run-id
      if (payload.metadata['temporal-run-id']) {
        context.runId = Buffer.from(
          payload.metadata['temporal-run-id']
        ).toString('utf-8');
      }

      // Check for temporal-namespace
      if (payload.metadata['temporal-namespace']) {
        context.namespace = Buffer.from(
          payload.metadata['temporal-namespace']
        ).toString('utf-8');
      }

      // If we found all context, we can stop searching
      if (
        context.workflowId !== 'unknown' &&
        context.runId !== 'unknown' &&
        context.namespace !== 'default'
      ) {
        break;
      }
    }

    return context;
  }
}
