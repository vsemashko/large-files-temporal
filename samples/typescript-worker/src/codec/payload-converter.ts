import {
  DefaultPayloadConverter,
  CompositePayloadConverter,
  PayloadCodec,
} from '@temporalio/common';
import { RemoteCodec } from './remote-codec';

// Create the payload converter with remote codec
const codecServerUrl = process.env.CODEC_SERVER_URL || 'http://localhost:8080';
const remoteCodec: PayloadCodec = new RemoteCodec(codecServerUrl);

export const payloadConverter = new CompositePayloadConverter(
  new DefaultPayloadConverter(),
  remoteCodec
);
