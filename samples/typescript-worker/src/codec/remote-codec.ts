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
          namespace: 'default',
          workflowId: 'unknown',
          runId: 'unknown',
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
}
