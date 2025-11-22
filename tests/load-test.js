import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { randomString } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const errorRate = new Rate('errors');
const encodeLatency = new Trend('encode_latency');
const decodeLatency = new Trend('decode_latency');
const largePayloadLatency = new Trend('large_payload_latency');
const payloadsSent = new Counter('payloads_sent');
const payloadsReceived = new Counter('payloads_received');

// Configuration
const CODEC_SERVER_URL = __ENV.CODEC_SERVER_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || '';
const SMALL_PAYLOAD_SIZE = 1024; // 1KB
const MEDIUM_PAYLOAD_SIZE = 512 * 1024; // 512KB
const LARGE_PAYLOAD_SIZE = 3 * 1024 * 1024; // 3MB (exceeds 2MB threshold)

export const options = {
  stages: [
    // Warmup
    { duration: '1m', target: 5 },

    // Gradual ramp-up
    { duration: '2m', target: 10 },
    { duration: '3m', target: 20 },
    { duration: '3m', target: 30 },
    { duration: '3m', target: 50 },

    // Peak load
    { duration: '5m', target: 100 },

    // Stress test (optional)
    { duration: '2m', target: 150 },

    // Cool down
    { duration: '2m', target: 50 },
    { duration: '2m', target: 10 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    // 95% of requests must complete within 2s
    'http_req_duration': ['p(95)<2000'],

    // 99% of requests must complete within 5s
    'http_req_duration': ['p(99)<5000'],

    // Error rate must be below 5%
    'errors': ['rate<0.05'],

    // Encode latency thresholds
    'encode_latency': ['p(95)<2000', 'p(99)<5000'],

    // Decode latency thresholds
    'decode_latency': ['p(95)<2000', 'p(99)<5000'],

    // Large payload handling
    'large_payload_latency': ['p(95)<5000', 'p(99)<10000'],

    // HTTP errors
    'http_req_failed': ['rate<0.05'],
  },
};

// Generate base64 encoded random data
function generatePayload(size) {
  const data = randomString(size);
  return Buffer.from(data).toString('base64');
}

// Common headers
function getHeaders() {
  const headers = {
    'Content-Type': 'application/json',
  };

  if (API_KEY) {
    headers['X-API-Key'] = API_KEY;
  }

  return headers;
}

// Test encode endpoint with small payload
function testSmallPayloadEncode() {
  const payload = {
    payloads: [{
      metadata: {
        encoding: Buffer.from('json/plain').toString('base64'),
      },
      data: generatePayload(SMALL_PAYLOAD_SIZE),
    }],
    namespace: 'load-test',
    workflowId: `workflow-${__VU}-${__ITER}`,
    runId: `run-${__VU}-${__ITER}`,
  };

  const startTime = new Date();
  const res = http.post(
    `${CODEC_SERVER_URL}/encode`,
    JSON.stringify(payload),
    { headers: getHeaders() }
  );
  const duration = new Date() - startTime;

  const success = check(res, {
    'encode status 200': (r) => r.status === 200,
    'encode has payloads': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payloads && body.payloads.length > 0;
      } catch (e) {
        return false;
      }
    },
    'encode completes quickly': () => duration < 1000,
  });

  errorRate.add(!success);
  encodeLatency.add(duration);
  payloadsSent.add(1);

  if (success) {
    payloadsReceived.add(1);
    return JSON.parse(res.body);
  }

  return null;
}

// Test decode endpoint
function testDecode(encodedPayloads) {
  if (!encodedPayloads || !encodedPayloads.payloads) {
    return false;
  }

  const payload = {
    payloads: encodedPayloads.payloads,
  };

  const startTime = new Date();
  const res = http.post(
    `${CODEC_SERVER_URL}/decode`,
    JSON.stringify(payload),
    { headers: getHeaders() }
  );
  const duration = new Date() - startTime;

  const success = check(res, {
    'decode status 200': (r) => r.status === 200,
    'decode has payloads': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payloads && body.payloads.length > 0;
      } catch (e) {
        return false;
      }
    },
    'decode completes quickly': () => duration < 1000,
  });

  errorRate.add(!success);
  decodeLatency.add(duration);

  return success;
}

// Test large payload (will be stored to S3)
function testLargePayload() {
  const payload = {
    payloads: [{
      metadata: {
        encoding: Buffer.from('binary/octet-stream').toString('base64'),
      },
      data: generatePayload(LARGE_PAYLOAD_SIZE),
    }],
    namespace: 'load-test',
    workflowId: `workflow-large-${__VU}-${__ITER}`,
    runId: `run-large-${__VU}-${__ITER}`,
  };

  const startTime = new Date();
  const res = http.post(
    `${CODEC_SERVER_URL}/encode`,
    JSON.stringify(payload),
    {
      headers: getHeaders(),
      timeout: '30s', // Longer timeout for large payloads
    }
  );
  const duration = new Date() - startTime;

  const success = check(res, {
    'large encode status 200': (r) => r.status === 200,
    'large encode has reference': (r) => {
      try {
        const body = JSON.parse(r.body);
        // Large payloads should have S3 reference
        return body.payloads && body.payloads.length > 0;
      } catch (e) {
        return false;
      }
    },
    'large encode completes': () => duration < 10000,
  });

  errorRate.add(!success);
  largePayloadLatency.add(duration);
  payloadsSent.add(1);

  if (success) {
    payloadsReceived.add(1);
  }
}

// Test multiple payloads in one request
function testMultiplePayloads() {
  const payloads = [];
  for (let i = 0; i < 5; i++) {
    payloads.push({
      metadata: {
        encoding: Buffer.from('json/plain').toString('base64'),
        index: Buffer.from(String(i)).toString('base64'),
      },
      data: generatePayload(SMALL_PAYLOAD_SIZE),
    });
  }

  const payload = {
    payloads: payloads,
    namespace: 'load-test',
    workflowId: `workflow-multi-${__VU}-${__ITER}`,
    runId: `run-multi-${__VU}-${__ITER}`,
  };

  const res = http.post(
    `${CODEC_SERVER_URL}/encode`,
    JSON.stringify(payload),
    { headers: getHeaders() }
  );

  const success = check(res, {
    'multi encode status 200': (r) => r.status === 200,
    'multi encode correct count': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payloads && body.payloads.length === 5;
      } catch (e) {
        return false;
      }
    },
  });

  errorRate.add(!success);
  payloadsSent.add(5);

  if (success) {
    payloadsReceived.add(5);
  }
}

// Test health endpoint
function testHealth() {
  const res = http.get(`${CODEC_SERVER_URL}/health`);

  check(res, {
    'health status 200': (r) => r.status === 200,
    'health status healthy': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.status === 'healthy';
      } catch (e) {
        return false;
      }
    },
  });
}

// Test medium payload
function testMediumPayload() {
  const payload = {
    payloads: [{
      metadata: {
        encoding: Buffer.from('json/plain').toString('base64'),
      },
      data: generatePayload(MEDIUM_PAYLOAD_SIZE),
    }],
    namespace: 'load-test',
    workflowId: `workflow-medium-${__VU}-${__ITER}`,
    runId: `run-medium-${__VU}-${__ITER}`,
  };

  const res = http.post(
    `${CODEC_SERVER_URL}/encode`,
    JSON.stringify(payload),
    { headers: getHeaders() }
  );

  const success = check(res, {
    'medium encode status 200': (r) => r.status === 200,
  });

  errorRate.add(!success);
  payloadsSent.add(1);

  if (success) {
    payloadsReceived.add(1);
  }
}

// Main test scenario
export default function() {
  // Determine which test to run based on VU and iteration
  const testType = __ITER % 10;

  switch (testType) {
    case 0:
      // 10% - Health check
      testHealth();
      sleep(1);
      break;

    case 1:
    case 2:
    case 3:
    case 4:
      // 40% - Small payload encode/decode roundtrip
      const encoded = testSmallPayloadEncode();
      if (encoded) {
        testDecode(encoded);
      }
      sleep(0.5);
      break;

    case 5:
    case 6:
      // 20% - Medium payload
      testMediumPayload();
      sleep(0.5);
      break;

    case 7:
      // 10% - Large payload (S3 storage)
      testLargePayload();
      sleep(2); // Longer sleep for large payloads
      break;

    case 8:
    case 9:
      // 20% - Multiple payloads
      testMultiplePayloads();
      sleep(0.5);
      break;
  }
}

// Setup function (runs once per VU)
export function setup() {
  console.log(`Starting load test against ${CODEC_SERVER_URL}`);

  // Verify server is accessible
  const res = http.get(`${CODEC_SERVER_URL}/health`);
  if (res.status !== 200) {
    console.error(`Server not accessible: ${res.status}`);
    return { serverReady: false };
  }

  console.log('Server is ready');
  return { serverReady: true };
}

// Teardown function (runs once after all VUs complete)
export function teardown(data) {
  if (!data.serverReady) {
    console.log('Server was not ready, test results may be invalid');
    return;
  }

  console.log('Load test completed');
  console.log(`Total payloads sent: ${payloadsSent.value}`);
  console.log(`Total payloads received: ${payloadsReceived.value}`);
}

// Thresholds summary
export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'load-test-results.json': JSON.stringify(data),
  };
}
